package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/VictoriaMetrics/metrics"
	"github.com/fess932/homeLab/internal/api"
	"github.com/fess932/homeLab/internal/assets"
	"github.com/fess932/homeLab/internal/auth"
	"github.com/fess932/homeLab/internal/config"
	"github.com/fess932/homeLab/internal/importer"
	"github.com/fess932/homeLab/internal/netguard"
	"github.com/fess932/homeLab/internal/probe"
	"github.com/fess932/homeLab/internal/secrets"
	"github.com/fess932/homeLab/internal/store"
	"github.com/fess932/homeLab/internal/tsdb"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sys/unix"
)

const shutdownBudget = 55 * time.Second

func Run(ctx context.Context, cfg config.Config, ui fs.FS, root *slog.Logger) error {
	log := root.With("component", "app")
	if err := prepareDataDir(cfg); err != nil {
		return err
	}
	unlock, err := lockDataDir(cfg.LockPath())
	if err != nil {
		return err
	}
	defer unlock()

	st, err := store.Open(ctx, cfg.DBPath())
	if err != nil {
		return err
	}
	defer st.Close()

	box, err := secrets.LoadOrCreateKey(cfg.SecretKeyFile)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(cfg.RuntimeDir); err != nil {
		return fmt.Errorf("runtime-каталог: %w", err)
	}
	if err := os.MkdirAll(cfg.RuntimeDir, 0o700); err != nil {
		return fmt.Errorf("runtime-каталог: %w", err)
	}

	setup, err := newSetupState(ctx, st, cfg.SetupTokenPath(), log)
	if err != nil {
		return err
	}

	scheduler := probe.NewScheduler(probe.NetProber{UserAgent: "HomeDeck/" + cfg.Version}, root.With("component", "scheduler"))
	syncChecks := func() {
		checks, err := st.ListChecks(context.Background())
		if err != nil {
			log.Error("load checks", "err", err)
			return
		}
		scheduler.Sync(checks)
	}
	syncChecks()

	sup := tsdb.NewSupervisor(tsdb.Options{
		Binary:          cfg.VMBinary,
		DataDir:         cfg.MetricsDir(),
		Listen:          cfg.VMListen,
		Retention:       cfg.Retention,
		ScrapeConfig:    filepath.Join(cfg.RuntimeDir, "scrape.yaml"),
		MinFreeDisk:     cfg.MinFreeDisk,
		MemoryPercent:   cfg.VMMemoryPercent,
		MaxScrapeSize:   "16MiB",
		SeriesPerTarget: tsdb.SeriesPerTarget,
	}, root.With("component", "tsdb"))
	rec := &tsdb.Reconciler{
		Store: st, Box: box, Sup: sup, Binary: cfg.VMBinary, RuntimeDir: cfg.RuntimeDir, RevisionsDir: cfg.RevisionsDir(),
		InternalListen: cfg.InternalListen, VMListen: cfg.VMListen, EgressProxy: cfg.EgressListen, Log: root.With("component", "reconciler"),
	}
	if err := rec.Prepare(ctx); err != nil {
		return fmt.Errorf("конфигурация сбора: %w", err)
	}
	sup.OnReady(rec.Kick)
	client := tsdb.NewClient(cfg.VMListen)
	watcher := tsdb.NewWatcher(client, st, sup, root.With("component", "watcher"))
	assetSvc := &assets.Service{Dir: cfg.AssetsDir(), Store: st}
	disk := newDiskMeter(cfg)
	imp := &importer.Service{Store: st, Assets: assetSvc, RevisionsDir: cfg.RevisionsDir(), OnApplied: func() {
		syncChecks()
		rec.Kick()
	}}

	apiSrv := api.New(api.Deps{
		Config: cfg, Store: st, Box: box, Scheduler: scheduler, Supervisor: sup, Reconciler: rec, Watcher: watcher,
		TSDB: client, Assets: assetSvc, Importer: imp, UI: ui, Log: root.With("component", "api"),
		SetupToken: setup.token, SetupDone: setup.done, OnChecks: syncChecks, Disk: disk.usage,
	})

	internalMux := http.NewServeMux()
	internalMux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		metrics.WritePrometheus(w, true)
		d := disk.usage()
		fmt.Fprintf(w, "homedeck_disk_total_bytes %d\nhomedeck_disk_free_bytes %d\nhomedeck_disk_metrics_bytes %d\nhomedeck_disk_app_bytes %d\n", d.Total, d.Free, d.Metrics, d.App)
		fmt.Fprintf(w, "homedeck_tsdb_restarts_total %d\n", sup.Status().Restarts)
		scheduler.WriteMetrics(w)
	})
	metrics.NewGauge(`homedeck_build_info{version="`+cfg.Version+`"}`, func() float64 { return 1 })

	servers := []*http.Server{
		{Addr: cfg.Listen, Handler: apiSrv, ReadHeaderTimeout: 10 * time.Second, WriteTimeout: 2 * time.Minute, IdleTimeout: 2 * time.Minute, ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn)},
		{Addr: cfg.InternalListen, Handler: internalMux, ReadHeaderTimeout: 5 * time.Second},
		{Addr: cfg.EgressListen, Handler: netguard.NewProxy(root.With("component", "egress")), ReadHeaderTimeout: 10 * time.Second},
	}
	listeners := make([]net.Listener, len(servers))
	for i, srv := range servers {
		if listeners[i], err = net.Listen("tcp", srv.Addr); err != nil {
			for _, l := range listeners[:i] {
				l.Close()
			}
			return fmt.Errorf("порт %s: %w", srv.Addr, err)
		}
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	g, gctx := errgroup.WithContext(runCtx)
	tsdbCtx, stopTSDB := context.WithCancel(context.Background())
	defer stopTSDB()
	tsdbDone := make(chan error, 1)
	go func() { tsdbDone <- sup.Run(tsdbCtx) }()
	g.Go(func() error {
		select {
		case err := <-tsdbDone:
			tsdbDone <- err
			return err
		case <-gctx.Done():
			return nil
		}
	})
	for i, srv := range servers {
		g.Go(func() error {
			if err := srv.Serve(listeners[i]); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("HTTP %s: %w", srv.Addr, err)
			}
			return nil
		})
	}
	g.Go(func() error { rec.Run(gctx); return nil })
	g.Go(func() error { watcher.Run(gctx); return nil })
	g.Go(func() error { housekeeping(gctx, st, log); return nil })
	log.Info("homedeck started", "listen", cfg.Listen, "version", cfg.Version, "data", cfg.DataDir, "retention", cfg.Retention)

	<-gctx.Done()
	log.Info("shutting down")
	deadline, cancelDeadline := context.WithTimeout(context.Background(), shutdownBudget)
	defer cancelDeadline()
	scheduler.Stop()
	httpCtx, cancelHTTP := context.WithTimeout(deadline, 10*time.Second)
	var wg sync.WaitGroup
	for _, srv := range servers {
		wg.Go(func() { _ = srv.Shutdown(httpCtx) })
	}
	wg.Wait()
	cancelHTTP()
	cancel()
	stopTSDB()
	var runErr error
	select {
	case runErr = <-tsdbDone:
	case <-deadline.Done():
		runErr = errors.New("TSDB не остановилась за отведённое время")
	}
	if err := g.Wait(); err != nil {
		runErr = err
	}
	if ctx.Err() != nil && errors.Is(runErr, context.Canceled) {
		runErr = nil
	}
	log.Info("stopped")
	return runErr
}

func prepareDataDir(cfg config.Config) error {
	for _, d := range []string{cfg.DataDir, cfg.AssetsDir(), cfg.MetricsDir(), cfg.RevisionsDir()} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return fmt.Errorf("каталог %s: %w", d, err)
		}
	}
	probe := filepath.Join(cfg.DataDir, ".write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return fmt.Errorf("нет прав на запись в %s (uid %d): %w", cfg.DataDir, os.Getuid(), err)
	}
	return os.Remove(probe)
}

func lockDataDir(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("каталог данных уже используется другим экземпляром HomeDeck: %w", err)
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		f.Close()
	}, nil
}

type setupState struct {
	mu      sync.Mutex
	path    string
	value   string
	pending bool
}

func newSetupState(ctx context.Context, st *store.Store, path string, log *slog.Logger) (*setupState, error) {
	s := &setupState{path: path}
	has, err := st.HasUsers(ctx)
	if err != nil {
		return nil, err
	}
	if has {
		_ = os.Remove(path)
		return s, nil
	}
	b, err := os.ReadFile(path)
	switch {
	case err == nil && len(strings.TrimSpace(string(b))) >= 32:
		s.value = strings.TrimSpace(string(b))
	case err == nil || errors.Is(err, fs.ErrNotExist):
		s.value = auth.RandomToken()
		if err := os.WriteFile(path, []byte(s.value+"\n"), 0o600); err != nil {
			return nil, fmt.Errorf("setup-token: %w", err)
		}
	default:
		return nil, fmt.Errorf("setup-token: %w", err)
	}
	s.pending = true
	log.Warn("первичная настройка не выполнена: откройте UI и введите setup-token, получить его можно командой `homedeck setup-token` внутри контейнера")
	return s, nil
}

func (s *setupState) token() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, s.pending
}

func (s *setupState) done() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending, s.value = false, ""
	_ = os.Remove(s.path)
}

func housekeeping(ctx context.Context, st *store.Store, log *slog.Logger) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if err := st.PurgeSessions(ctx); err != nil && ctx.Err() == nil {
			log.Warn("purge sessions", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

type diskMeter struct {
	cfg     config.Config
	mu      sync.Mutex
	cached  api.DiskUsage
	updated time.Time
}

func newDiskMeter(cfg config.Config) *diskMeter { return &diskMeter{cfg: cfg} }

func (d *diskMeter) usage() api.DiskUsage {
	d.mu.Lock()
	defer d.mu.Unlock()
	if time.Since(d.updated) < 30*time.Second {
		return d.cached
	}
	var u api.DiskUsage
	var sfs unix.Statfs_t
	if unix.Statfs(d.cfg.DataDir, &sfs) == nil {
		u.Total = int64(sfs.Blocks) * int64(sfs.Bsize)
		u.Free = int64(sfs.Bavail) * int64(sfs.Bsize)
	}
	u.Metrics = dirSize(d.cfg.MetricsDir())
	u.App = dirSize(d.cfg.AssetsDir()) + dirSize(d.cfg.RevisionsDir())
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if st, err := os.Stat(d.cfg.DBPath() + suffix); err == nil {
			u.App += st.Size()
		}
	}
	d.cached, d.updated = u, time.Now()
	return u
}

func dirSize(root string) int64 {
	var n int64
	_ = filepath.WalkDir(root, func(_ string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if e.Type().IsRegular() {
			if info, err := e.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

func Healthcheck(ctx context.Context, listen string) error {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(host, port)+"/readyz", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("not ready: %s", strings.TrimSpace(string(body)))
	}
	return nil
}
