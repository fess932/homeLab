package tsdb

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/secrets"
	"github.com/fess932/homeLab/internal/store"
)

type Reconciler struct {
	Store          *store.Store
	Box            *secrets.Box
	Sup            *Supervisor
	Binary         string
	RuntimeDir     string
	RevisionsDir   string
	InternalListen string
	VMListen       string
	EgressProxy    string
	Log            *slog.Logger

	kick    chan struct{}
	once    sync.Once
	mu      sync.Mutex
	current []byte
}

func (r *Reconciler) init() {
	r.once.Do(func() { r.kick = make(chan struct{}, 1) })
}

func (r *Reconciler) ConfigPath() string { return filepath.Join(r.RuntimeDir, "scrape.yaml") }

func (r *Reconciler) Kick() {
	r.init()
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

func (r *Reconciler) Prepare(ctx context.Context) error {
	r.init()
	if err := os.MkdirAll(filepath.Join(r.RuntimeDir, "secrets"), 0o700); err != nil {
		return err
	}
	cfg, files, desired, err := r.build(ctx)
	if err == nil {
		err = r.validate(ctx, cfg, files)
	}
	if err != nil {
		r.Log.Error("stored scrape config rejected, starting with system sources only", "err", err)
		fallback, _, _ := BuildScrapeConfig(r.input(nil, nil))
		if werr := writeAtomic(r.ConfigPath(), fallback); werr != nil {
			return werr
		}
		r.mu.Lock()
		r.current = fallback
		r.mu.Unlock()
		return r.Store.SetConfigApplied(ctx, 0, err.Error())
	}
	if err := r.install(cfg, files); err != nil {
		return err
	}
	r.archive(desired, cfg)
	return r.Store.SetConfigApplied(ctx, desired, "")
}

func (r *Reconciler) Run(ctx context.Context) {
	r.init()
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.kick:
		case <-t.C:
		}
		if err := r.Apply(ctx); err != nil && ctx.Err() == nil {
			r.Log.Error("scrape config apply failed", "err", err)
		}
	}
}

func (r *Reconciler) Apply(ctx context.Context) error {
	st, err := r.Store.ConfigState(ctx)
	if err != nil {
		return err
	}
	if st.Applied == st.Desired && st.Error == "" {
		return nil
	}
	if !r.Sup.Ready() {
		return errors.New("TSDB не готова, применение отложено")
	}
	cfg, files, desired, err := r.build(ctx)
	if err == nil {
		err = r.validate(ctx, cfg, files)
	}
	if err == nil {
		err = r.install(cfg, files)
	}
	if err == nil {
		err = r.reload(ctx)
	}
	if err != nil {
		r.mu.Lock()
		prev := r.current
		r.mu.Unlock()
		if prev != nil {
			_ = writeAtomic(r.ConfigPath(), prev)
		}
		_ = r.Store.SetConfigApplied(ctx, 0, err.Error())
		return err
	}
	r.archive(desired, cfg)
	r.Log.Info("scrape config applied", "revision", desired)
	return r.Store.SetConfigApplied(ctx, desired, "")
}

func (r *Reconciler) input(sources []model.Source, sec map[string]secrets.Payload) ScrapeInput {
	return ScrapeInput{
		Sources:        sources,
		Secrets:        sec,
		InternalListen: r.InternalListen,
		VMListen:       r.VMListen,
		EgressProxy:    r.EgressProxy,
		RuntimeDir:     r.RuntimeDir,
	}
}

func (r *Reconciler) build(ctx context.Context) ([]byte, []RuntimeFile, int64, error) {
	st, err := r.Store.ConfigState(ctx)
	if err != nil {
		return nil, nil, 0, err
	}
	sources, err := r.Store.ListSources(ctx)
	if err != nil {
		return nil, nil, 0, err
	}
	sec := map[string]secrets.Payload{}
	for _, s := range sources {
		if s.SecretID == nil || (s.Enabled != nil && !*s.Enabled) {
			continue
		}
		if _, done := sec[*s.SecretID]; done {
			continue
		}
		rec, err := r.Store.GetSecret(ctx, *s.SecretID)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("секрет %s: %w", *s.SecretID, err)
		}
		p, err := r.Box.Open(rec.ID, rec.Payload)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("секрет %s: %w", rec.ID, err)
		}
		sec[rec.ID] = p
	}
	cfg, files, err := BuildScrapeConfig(r.input(sources, sec))
	return cfg, files, st.Desired, err
}

func (r *Reconciler) validate(ctx context.Context, cfg []byte, files []RuntimeFile) error {
	dir, err := os.MkdirTemp(r.RuntimeDir, "check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	staged := cfg
	for _, f := range files {
		rel, err := filepath.Rel(r.RuntimeDir, f.Path)
		if err != nil {
			return err
		}
		tmp := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(tmp), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(tmp, f.Content, 0o600); err != nil {
			return err
		}
		staged = bytes.ReplaceAll(staged, []byte(f.Path), []byte(tmp))
	}
	path := filepath.Join(dir, "scrape.yaml")
	if err := os.WriteFile(path, staged, 0o600); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, r.Binary, "-promscrape.config="+path, "-promscrape.config.dryRun", "-promscrape.config.strictParse", "-loggerFormat=default").CombinedOutput()
	if err != nil {
		return fmt.Errorf("конфигурация сбора отклонена: %s", dryRunError(out, err))
	}
	return nil
}

func dryRunError(out []byte, err error) string {
	sc := bufio.NewScanner(bytes.NewReader(out))
	var lines []string
	for sc.Scan() {
		l := sc.Text()
		if strings.Contains(l, "fatal") || strings.Contains(l, "error") || strings.HasPrefix(l, "  line") {
			if i := strings.Index(l, "cannot "); i >= 0 {
				l = l[i:]
			}
			lines = append(lines, strings.TrimSpace(l))
		}
	}
	if len(lines) == 0 {
		return err.Error()
	}
	return strings.Join(lines, "; ")
}

func (r *Reconciler) install(cfg []byte, files []RuntimeFile) error {
	keep := map[string]bool{}
	for _, f := range files {
		if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
			return err
		}
		if err := writeAtomic(f.Path, f.Content); err != nil {
			return err
		}
		keep[filepath.Dir(f.Path)] = true
	}
	if err := writeAtomic(r.ConfigPath(), cfg); err != nil {
		return err
	}
	entries, _ := os.ReadDir(filepath.Join(r.RuntimeDir, "secrets"))
	for _, e := range entries {
		p := filepath.Join(r.RuntimeDir, "secrets", e.Name())
		if !keep[p] {
			_ = os.RemoveAll(p)
		}
	}
	r.mu.Lock()
	r.current = cfg
	r.mu.Unlock()
	return nil
}

func (r *Reconciler) reload(ctx context.Context) error {
	before, err := r.reloadMetrics(ctx)
	if err != nil {
		return fmt.Errorf("состояние TSDB до reload: %w", err)
	}
	requested := time.Now().Unix()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+r.VMListen+"/-/reload", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("reload TSDB: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("reload TSDB: %s", resp.Status)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
		m, err := r.reloadMetrics(ctx)
		if err != nil {
			continue
		}
		if m.errors > before.errors {
			return errors.New("TSDB отклонила новую конфигурацию сбора, продолжает работать прежняя")
		}
		if m.successful && (m.successTS >= requested || m.reloads > before.reloads) {
			return nil
		}
	}
	return errors.New("TSDB не подтвердила применение конфигурации за 10 секунд")
}

type reloadState struct {
	successful bool
	successTS  int64
	errors     int64
	reloads    int64
}

func (r *Reconciler) reloadMetrics(ctx context.Context) (reloadState, error) {
	var st reloadState
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+r.VMListen+"/metrics", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return st, err
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		name, val, ok := strings.Cut(sc.Text(), " ")
		if !ok {
			continue
		}
		v, _ := strconv.ParseFloat(val, 64)
		switch name {
		case "vm_promscrape_config_last_reload_successful":
			st.successful = v == 1
		case "vm_promscrape_config_last_reload_success_timestamp_seconds":
			st.successTS = int64(v)
		case "vm_promscrape_config_reloads_errors_total":
			st.errors = int64(v)
		case "vm_promscrape_config_reloads_total":
			st.reloads = int64(v)
		}
	}
	return st, sc.Err()
}

func (r *Reconciler) archive(revision int64, cfg []byte) {
	if r.RevisionsDir == "" {
		return
	}
	if err := os.MkdirAll(r.RevisionsDir, 0o700); err != nil {
		r.Log.Warn("config archive", "err", err)
		return
	}
	name := filepath.Join(r.RevisionsDir, fmt.Sprintf("scrape-%06d.yaml", revision))
	if err := writeAtomic(name, cfg); err != nil {
		r.Log.Warn("config archive", "err", err)
		return
	}
	matches, _ := filepath.Glob(filepath.Join(r.RevisionsDir, "scrape-*.yaml"))
	slices.Sort(matches)
	for len(matches) > 20 {
		_ = os.Remove(matches[0])
		matches = matches[1:]
	}
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
