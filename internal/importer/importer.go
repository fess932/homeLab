package importer

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fess932/homeLab/internal/auth"
	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/store"
)

const tokenTTL = 10 * time.Minute

var ErrTokenExpired = errors.New("предпросмотр устарел или не найден, выполните проверку заново")

type Warning struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type Change struct {
	Entity string `json:"entity"`
	Action string `json:"action"`
	Name   string `json:"name"`
}

type Preview struct {
	Token      string    `json:"token"`
	Format     string    `json:"format"`
	Applied    bool      `json:"applied"`
	RevisionID *int64    `json:"revision_id"`
	Changes    []Change  `json:"changes"`
	Warnings   []Warning `json:"warnings"`
}

type pending struct {
	format  string
	hash    [32]byte
	version string
	scope   store.ReplaceScope
	snap    store.Snapshot
	homer   *homerPlan
	preview Preview
	expires time.Time
}

type Service struct {
	Store        *store.Store
	Assets       assetSaver
	RevisionsDir string
	OnApplied    func()

	mu      sync.Mutex
	pending map[string]*pending
}

func (s *Service) Preview(ctx context.Context, format string, data, zipData []byte) (Preview, error) {
	version, err := s.Store.DataVersion(ctx)
	if err != nil {
		return Preview{}, err
	}
	cur, err := s.Store.Snapshot(ctx)
	if err != nil {
		return Preview{}, err
	}
	p := &pending{format: format, hash: sha256.Sum256(append(append([]byte{}, data...), zipData...)), version: version, expires: time.Now().Add(tokenTTL)}
	var warns []Warning
	switch format {
	case "homedeck":
		doc, err := parseDocument(data)
		if err != nil {
			return Preview{}, model.Invalid("file", err.Error())
		}
		env, err := s.env(ctx, cur)
		if err != nil {
			return Preview{}, err
		}
		snap, w, err := buildHomeDeck(doc, env)
		if err != nil {
			return Preview{}, err
		}
		warns = w
		p.snap = snap
		p.scope = store.ReplaceScope{Pages: true, Services: true, Sources: true, Devices: true, Presets: true}
	case "homer":
		plan, w, err := parseHomer(data, zipData, cur)
		if err != nil {
			if _, ok := errors.AsType[*model.ValidationError](err); ok {
				return Preview{}, err
			}
			return Preview{}, model.Invalid("file", err.Error())
		}
		warns = w
		p.snap = plan.snap
		p.homer = &plan
		p.scope = store.ReplaceScope{Pages: true, Services: true}
	default:
		return Preview{}, model.Invalid("format", "homedeck или homer")
	}
	if warns == nil {
		warns = []Warning{}
	}
	p.preview = Preview{Token: auth.RandomToken(), Format: format, Changes: changes(cur, p.snap, p.scope), Warnings: warns}
	s.mu.Lock()
	if s.pending == nil {
		s.pending = map[string]*pending{}
	}
	now := time.Now()
	for k, v := range s.pending {
		if now.After(v.expires) {
			delete(s.pending, k)
		}
	}
	s.pending[p.preview.Token] = p
	s.mu.Unlock()
	return p.preview, nil
}

func changes(cur, next store.Snapshot, scope store.ReplaceScope) []Change {
	out := []Change{}
	if scope.Pages {
		out = append(out, diff("page", cur.Pages, next.Pages, func(p model.Page) string { return p.ID }, func(p model.Page) string { return p.Title })...)
	}
	if scope.Services {
		out = append(out, diff("service", cur.Services, next.Services, func(s model.Service) string { return s.ID }, func(s model.Service) string { return s.Name })...)
		out = append(out, diff("check", cur.Checks, next.Checks, func(c model.Check) string { return c.ID }, func(c model.Check) string { return c.Target })...)
	}
	if scope.Sources {
		out = append(out, diff("source", cur.Sources, next.Sources, func(s model.Source) string { return s.ID }, func(s model.Source) string { return s.Name })...)
	}
	if scope.Devices {
		out = append(out, diff("device", cur.Devices, next.Devices, func(d model.Device) string { return d.ID }, func(d model.Device) string { return d.Name })...)
	}
	if scope.Presets {
		out = append(out, diff("preset", cur.Presets, next.Presets, func(p model.Preset) string { return p.ID }, func(p model.Preset) string { return p.Title })...)
	}
	if scope.Pages && cur.Settings.Title != next.Settings.Title {
		out = append(out, Change{Entity: "settings", Action: "replace", Name: next.Settings.Title})
	}
	return out
}

func (s *Service) env(ctx context.Context, cur store.Snapshot) (Env, error) {
	env := Env{Current: cur, SecretIDs: map[string]bool{}, AssetIDs: map[string]bool{}}
	secrets, err := s.Store.ListSecrets(ctx)
	if err != nil {
		return env, err
	}
	for _, sc := range secrets {
		env.SecretIDs[sc.ID] = true
	}
	assets, err := s.Store.ListAssets(ctx)
	if err != nil {
		return env, err
	}
	for _, a := range assets {
		env.AssetIDs[a.ID] = true
	}
	return env, nil
}

func (s *Service) Apply(ctx context.Context, token string) (Preview, error) {
	s.mu.Lock()
	p, ok := s.pending[token]
	if ok {
		delete(s.pending, token)
	}
	s.mu.Unlock()
	if !ok || time.Now().After(p.expires) {
		return Preview{}, ErrTokenExpired
	}
	version, err := s.Store.DataVersion(ctx)
	if err != nil {
		return Preview{}, err
	}
	if version != p.version {
		return Preview{}, fmt.Errorf("%w: настройки изменились после предпросмотра, выполните проверку заново", model.ErrConflict)
	}
	revID, err := s.snapshotRevision(ctx, "import "+p.format)
	if err != nil {
		return Preview{}, fmt.Errorf("ревизия перед импортом: %w", err)
	}
	if p.homer != nil {
		w, err := p.homer.materialize(ctx, s.Assets)
		if err != nil {
			return Preview{}, err
		}
		p.snap = p.homer.snap
		p.preview.Warnings = append(p.preview.Warnings, w...)
		if version, err = s.Store.DataVersion(ctx); err != nil {
			return Preview{}, err
		}
	}
	if err := s.Store.Replace(ctx, p.scope, p.snap, version); err != nil {
		if errors.Is(err, model.ErrConflict) {
			return Preview{}, fmt.Errorf("%w: настройки изменились во время импорта, выполните проверку заново", model.ErrConflict)
		}
		return Preview{}, err
	}
	if s.OnApplied != nil {
		s.OnApplied()
	}
	out := p.preview
	out.Token, out.Applied, out.RevisionID = "", true, &revID
	return out, nil
}

func (s *Service) Export(ctx context.Context) ([]byte, error) {
	snap, err := s.Store.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return MarshalYAML(Export(snap, time.Now()))
}

func (s *Service) snapshotRevision(ctx context.Context, reason string) (int64, error) {
	data, err := s.Export(ctx)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(s.RevisionsDir, 0o700); err != nil {
		return 0, err
	}
	name := fmt.Sprintf("snapshot-%s.yaml", time.Now().UTC().Format("20060102T150405.000Z"))
	path := filepath.Join(s.RevisionsDir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return 0, err
	}
	return s.Store.AddRevision(ctx, reason, name)
}
