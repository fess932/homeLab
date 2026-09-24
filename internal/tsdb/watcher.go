package tsdb

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/fess932/homeLab/internal/model"
)

type SourceStatusStore interface {
	SaveSourceStatus(ctx context.Context, id string, attempt, success time.Time, errText string) error
}

type Watcher struct {
	client *Client
	store  SourceStatusStore
	sup    *Supervisor
	log    *slog.Logger

	mu        sync.Mutex
	targets   map[string]Target
	success   map[string]time.Time
	persisted map[string]persisted
	updated   time.Time
}

type persisted struct {
	err     string
	attempt time.Time
}

func NewWatcher(c *Client, st SourceStatusStore, sup *Supervisor, log *slog.Logger) *Watcher {
	return &Watcher{client: c, store: st, sup: sup, log: log, targets: map[string]Target{}, success: map[string]time.Time{}, persisted: map[string]persisted{}}
}

func (w *Watcher) Run(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if !w.sup.Ready() {
			continue
		}
		targets, err := w.client.Targets(ctx)
		if err != nil {
			w.log.Debug("targets poll failed", "err", err)
			continue
		}
		w.update(ctx, targets)
	}
}

func (w *Watcher) update(ctx context.Context, targets []Target) {
	w.mu.Lock()
	next := map[string]Target{}
	var toSave []Target
	for _, t := range targets {
		next[t.SourceID] = t
		if t.Health == "up" && !t.LastScrape.IsZero() {
			w.success[t.SourceID] = t.LastScrape
		}
		p := w.persisted[t.SourceID]
		if t.SourceID != model.SystemSourceApp && t.SourceID != model.SystemSourceTSDB && !t.LastScrape.IsZero() &&
			(p.err != t.LastError || t.LastScrape.Sub(p.attempt) > time.Minute) {
			w.persisted[t.SourceID] = persisted{err: t.LastError, attempt: t.LastScrape}
			toSave = append(toSave, t)
		}
	}
	w.targets = next
	w.updated = time.Now()
	success := make(map[string]time.Time, len(toSave))
	for _, t := range toSave {
		success[t.SourceID] = w.success[t.SourceID]
	}
	w.mu.Unlock()
	for _, t := range toSave {
		if err := w.store.SaveSourceStatus(ctx, t.SourceID, t.LastScrape, success[t.SourceID], t.LastError); err != nil {
			w.log.Warn("save source status", "source_id", t.SourceID, "err", err)
		}
	}
}

func (w *Watcher) Status(src model.Source) model.SourceStatus {
	st := src.Status
	if src.Enabled != nil && !*src.Enabled {
		st.State = "disabled"
		return st
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	t, ok := w.targets[src.ID]
	if !ok || t.LastScrape.IsZero() {
		st.State = model.StatePending
		return st
	}
	st.LastAttempt = model.TimePtr(t.LastScrape)
	if s, ok := w.success[src.ID]; ok {
		st.LastSuccess = model.TimePtr(s)
	}
	st.DurationMS = new(float64(t.Duration.Microseconds()) / 1000)
	st.Samples = new(t.Samples)
	st.Error = t.LastError
	if t.Health == "up" {
		st.State = model.StateUp
	} else {
		st.State = model.StateDown
	}
	return st
}
