package probe

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"math/rand/v2"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/fess932/homeLab/internal/model"
)

const (
	UpThreshold   = 2
	DownThreshold = 3
	MaxConcurrent = 10
)

// confirmInterval — как часто перепроверять, пока состояние не подтверждено порогом
// (новая проверка или смена «работает»/«не работает»): точка не ждёт несколько
// обычных интервалов. После подтверждения — снова обычный интервал проверки.
var confirmInterval = 15 * time.Second

type tracker struct {
	check       model.Check
	state       string
	successes   int
	failures    int
	started     time.Time
	lastRun     time.Time
	lastSuccess time.Time
	last        Result
	runs        int
}

func (t *tracker) record(r Result, at time.Time) {
	t.lastRun, t.last = at, r
	t.runs++
	if r.OK {
		t.lastSuccess = at
		t.successes++
		t.failures = 0
		if t.successes >= UpThreshold {
			t.state = model.StateUp
		}
	} else {
		t.failures++
		t.successes = 0
		if t.failures >= DownThreshold {
			t.state = model.StateDown
		}
	}
}

// unconfirmed — последние результаты расходятся с состоянием или его ещё нет.
func (t *tracker) unconfirmed() bool {
	return t.state == model.StateUnknown ||
		(t.successes > 0 && t.state != model.StateUp) ||
		(t.failures > 0 && t.state != model.StateDown)
}

func (t *tracker) status(now time.Time) model.CheckStatus {
	s := model.CheckStatus{
		State:       t.state,
		LastRun:     model.TimePtr(t.lastRun),
		LastSuccess: model.TimePtr(t.lastSuccess),
		Error:       t.last.Error,
	}
	if t.runs > 0 {
		s.DurationMS = new(float64(t.last.Duration.Microseconds()) / 1000)
		if t.last.HTTPStatus != 0 {
			s.HTTPStatus = new(t.last.HTTPStatus)
		}
	}
	switch {
	case t.successes > 0 && t.state != model.StateUp:
		s.Pending, s.Streak = new(model.StateUp), t.successes
	case t.failures > 0 && t.state != model.StateDown:
		s.Pending, s.Streak = new(model.StateDown), t.failures
	default:
		s.Streak = max(t.successes, t.failures)
	}
	if !*t.check.Enabled {
		s.State, s.Pending = model.StateDisabled, nil
		return s
	}
	ref := t.lastRun
	if ref.IsZero() {
		ref = t.started
	}
	if now.Sub(ref) > staleAfter(t.check) {
		s.State, s.Pending = model.StateStale, nil
	}
	return s
}

func staleAfter(c model.Check) time.Duration {
	return time.Duration(2*c.IntervalS+c.TimeoutS) * time.Second
}

type runner struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type Scheduler struct {
	prober Prober
	log    *slog.Logger
	now    func() time.Time
	sem    chan struct{}

	mu       sync.Mutex
	trackers map[string]*tracker
	runners  map[string]*runner
	ctx      context.Context
	stop     context.CancelFunc
}

func NewScheduler(p Prober, log *slog.Logger) *Scheduler {
	ctx, stop := context.WithCancel(context.Background())
	return &Scheduler{
		prober:   p,
		log:      log,
		now:      time.Now,
		sem:      make(chan struct{}, MaxConcurrent),
		trackers: map[string]*tracker{},
		runners:  map[string]*runner{},
		ctx:      ctx,
		stop:     stop,
	}
}

func sameTarget(a, b model.Check) bool {
	return a.Kind == b.Kind && a.Target == b.Target && a.ExpectedStatus == b.ExpectedStatus && a.CAPEM == b.CAPEM && *a.Enabled == *b.Enabled
}

func (s *Scheduler) Sync(checks []model.Check) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return
	}
	want := map[string]model.Check{}
	for _, c := range checks {
		want[c.ID] = c
	}
	for id, t := range s.trackers {
		c, ok := want[id]
		if ok && c.Revision == t.check.Revision {
			continue
		}
		s.stopRunner(id)
		if !ok {
			delete(s.trackers, id)
			continue
		}
		if !sameTarget(c, t.check) {
			delete(s.trackers, id)
		}
	}
	for id, c := range want {
		t, ok := s.trackers[id]
		if !ok {
			t = &tracker{check: c, state: model.StateUnknown, started: s.now()}
			s.trackers[id] = t
		}
		t.check = c
		if _, running := s.runners[id]; !running && *c.Enabled {
			s.startRunner(t)
		}
	}
}

func (s *Scheduler) stopRunner(id string) {
	if r, ok := s.runners[id]; ok {
		r.cancel()
		<-r.done
		delete(s.runners, id)
	}
}

func (s *Scheduler) startRunner(t *tracker) {
	ctx, cancel := context.WithCancel(s.ctx)
	r := &runner{cancel: cancel, done: make(chan struct{})}
	s.runners[t.check.ID] = r
	c := t.check
	go func() {
		defer close(r.done)
		interval := time.Duration(c.IntervalS) * time.Second
		jitter := time.Duration(rand.Int64N(int64(min(interval/10, time.Second)) + 1))
		timer := time.NewTimer(jitter)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		for {
			next := interval
			if s.runOnce(ctx, c) {
				next = min(confirmInterval, interval)
			}
			timer.Reset(next)
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
		}
	}()
}

// runOnce выполняет проверку и сообщает, нужно ли быстро перепроверить (состояние не подтверждено).
func (s *Scheduler) runOnce(ctx context.Context, c model.Check) bool {
	select {
	case s.sem <- struct{}{}:
	case <-ctx.Done():
		return false
	}
	res := s.prober.Probe(ctx, c)
	<-s.sem
	if ctx.Err() != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.trackers[c.ID]
	if !ok || t.check.Revision != c.Revision {
		return false
	}
	prev := t.state
	t.record(res, s.now())
	if prev != t.state {
		s.log.Info("check state changed", "check_id", c.ID, "service_id", c.ServiceID, "from", prev, "to", t.state, "error", res.Error)
	}
	return t.unconfirmed()
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	s.stop()
	ids := slices.Collect(maps.Keys(s.runners))
	for _, id := range ids {
		s.stopRunner(id)
	}
	s.mu.Unlock()
}

func (s *Scheduler) Status(checkID string) (model.CheckStatus, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.trackers[checkID]
	if !ok {
		return model.CheckStatus{State: model.StateUnknown}, false
	}
	return t.status(s.now()), true
}

func (s *Scheduler) WriteMetrics(w io.Writer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := slices.Sorted(maps.Keys(s.trackers))
	type line struct{ name, labels, value string }
	var lines []line
	for _, id := range ids {
		t := s.trackers[id]
		if t.runs == 0 || !*t.check.Enabled {
			continue
		}
		l := fmt.Sprintf(`service_id=%q,kind=%q`, t.check.ServiceID, t.check.Kind)
		lines = append(lines,
			line{"homedeck_probe_success", l, boolStr(t.last.OK)},
			line{"homedeck_probe_duration_seconds", l, strconv.FormatFloat(t.last.Duration.Seconds(), 'f', 6, 64)},
			line{"homedeck_probe_last_run_timestamp_seconds", l, strconv.FormatInt(t.lastRun.Unix(), 10)},
		)
		if !t.lastSuccess.IsZero() {
			lines = append(lines, line{"homedeck_probe_last_success_timestamp_seconds", l, strconv.FormatInt(t.lastSuccess.Unix(), 10)})
		}
		if t.check.Kind == "http" && t.last.HTTPStatus != 0 {
			lines = append(lines, line{"homedeck_probe_http_status_code", l, strconv.Itoa(t.last.HTTPStatus)})
		}
	}
	slices.SortStableFunc(lines, func(a, b line) int {
		if a.name < b.name {
			return -1
		}
		if a.name > b.name {
			return 1
		}
		return 0
	})
	help := map[string]string{
		"homedeck_probe_success":                        "1 if the last check succeeded",
		"homedeck_probe_duration_seconds":               "Duration of the last check",
		"homedeck_probe_last_run_timestamp_seconds":     "Unix time of the last completed check",
		"homedeck_probe_last_success_timestamp_seconds": "Unix time of the last successful check",
		"homedeck_probe_http_status_code":               "HTTP status code of the last check",
	}
	prev := ""
	for _, l := range lines {
		if l.name != prev {
			fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n", l.name, help[l.name], l.name)
			prev = l.name
		}
		fmt.Fprintf(w, "%s{%s} %s\n", l.name, l.labels, l.value)
	}
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
