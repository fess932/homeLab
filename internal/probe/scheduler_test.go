package probe

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fess932/homeLab/internal/model"
)

func check(id string) model.Check {
	return model.Check{ID: id, Revision: 1,
		ServiceID: "svc_" + id, Kind: "http", Target: "http://example", ExpectedStatus: "200-399",
		IntervalS: 30, TimeoutS: 5, Enabled: new(true)}
}

func TestTrackerThresholds(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	tr := &tracker{check: check("a"), state: model.StateUnknown, started: now}
	ok, fail := Result{OK: true}, Result{Error: "boom"}

	st := tr.status(now)
	if st.State != model.StateUnknown || st.Pending != nil {
		t.Fatalf("до первой проверки: %+v", st)
	}

	// Один успех не переводит в up: нужен порог 2 подряд, до него показывается счётчик.
	tr.record(ok, now)
	st = tr.status(now)
	if st.State != model.StateUnknown || st.Pending == nil || *st.Pending != model.StateUp || st.Streak != 1 {
		t.Fatalf("после 1 успеха: %+v", st)
	}
	tr.record(ok, now)
	if st = tr.status(now); st.State != model.StateUp || st.Pending != nil {
		t.Fatalf("после 2 успехов: %+v", st)
	}

	// Две ошибки подряд оставляют up со счётчиком к down.
	tr.record(fail, now)
	tr.record(fail, now)
	st = tr.status(now)
	if st.State != model.StateUp || st.Pending == nil || *st.Pending != model.StateDown || st.Streak != 2 {
		t.Fatalf("после 2 ошибок: %+v", st)
	}
	tr.record(fail, now)
	if st = tr.status(now); st.State != model.StateDown {
		t.Fatalf("после 3 ошибок: %+v", st)
	}

	// Успех после ошибок сбрасывает счётчик ошибок, но состояние down держится до порога.
	tr.record(ok, now)
	if st = tr.status(now); st.State != model.StateDown || *st.Pending != model.StateUp {
		t.Fatalf("1 успех после down: %+v", st)
	}
	tr.record(fail, now)
	tr.record(ok, now)
	if st = tr.status(now); st.State != model.StateDown {
		t.Fatalf("чередование не должно переводить в up: %+v", st)
	}
}

func TestTrackerStaleAndDisabled(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	tr := &tracker{check: check("a"), state: model.StateUnknown, started: now}
	tr.record(Result{OK: true}, now)
	tr.record(Result{OK: true}, now)

	// 2 интервала + timeout = 65 секунд; ровно на границе ещё не stale.
	if st := tr.status(now.Add(65 * time.Second)); st.State != model.StateUp {
		t.Fatalf("на границе: %s", st.State)
	}
	if st := tr.status(now.Add(66 * time.Second)); st.State != model.StateStale {
		t.Fatalf("успешный, но старый результат должен стать stale: %s", st.State)
	}

	// Проверка, ни разу не выполнившаяся, тоже устаревает относительно старта.
	fresh := &tracker{check: check("b"), state: model.StateUnknown, started: now}
	if st := fresh.status(now.Add(time.Minute)); st.State != model.StateUnknown {
		t.Fatalf("до порога: %s", st.State)
	}
	if st := fresh.status(now.Add(2 * time.Minute)); st.State != model.StateStale {
		t.Fatalf("без результатов: %s", st.State)
	}

	tr.check.Enabled = new(false)
	if st := tr.status(now.Add(time.Hour)); st.State != model.StateDisabled {
		t.Fatalf("выключенная: %s", st.State)
	}
}

type fakeProber struct {
	mu      sync.Mutex
	active  int
	peak    int
	calls   atomic.Int64
	block   time.Duration
	results func(model.Check) Result
}

func (f *fakeProber) Probe(ctx context.Context, c model.Check) Result {
	f.mu.Lock()
	f.active++
	f.peak = max(f.peak, f.active)
	f.mu.Unlock()
	f.calls.Add(1)
	select {
	case <-time.After(f.block):
	case <-ctx.Done():
	}
	f.mu.Lock()
	f.active--
	f.mu.Unlock()
	if f.results != nil {
		return f.results(c)
	}
	return Result{OK: true, Duration: time.Millisecond, HTTPStatus: 200}
}

func TestSchedulerConcurrencyLimit(t *testing.T) {
	fp := &fakeProber{block: 200 * time.Millisecond}
	s := NewScheduler(fp, slog.New(slog.DiscardHandler))
	defer s.Stop()
	var checks []model.Check
	for i := range 25 {
		c := check(string(rune('a' + i)))
		c.IntervalS = 5
		checks = append(checks, c)
	}
	s.Sync(checks)
	deadline := time.Now().Add(5 * time.Second)
	for fp.calls.Load() < 25 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	fp.mu.Lock()
	peak := fp.peak
	fp.mu.Unlock()
	if peak > MaxConcurrent {
		t.Fatalf("одновременно %d проверок, лимит %d", peak, MaxConcurrent)
	}
	if fp.calls.Load() < 25 {
		t.Fatalf("выполнено %d из 25", fp.calls.Load())
	}
}

func TestSchedulerSyncAndMetrics(t *testing.T) {
	fp := &fakeProber{}
	s := NewScheduler(fp, slog.New(slog.DiscardHandler))
	defer s.Stop()
	a, b := check("a"), check("b")
	b.Kind, b.Target, b.ExpectedStatus = "tcp", "host:22", ""
	s.Sync([]model.Check{a, b})
	waitRuns(t, s, "a", 1)
	waitRuns(t, s, "b", 1)

	var buf bytes.Buffer
	s.WriteMetrics(&buf)
	out := buf.String()
	for _, want := range []string{
		`homedeck_probe_success{service_id="svc_a",kind="http"} 1`,
		`homedeck_probe_http_status_code{service_id="svc_a",kind="http"} 200`,
		`homedeck_probe_success{service_id="svc_b",kind="tcp"} 1`,
		`# TYPE homedeck_probe_duration_seconds gauge`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("нет строки %q в\n%s", want, out)
		}
	}
	// Для TCP код ответа не публикуется.
	if strings.Contains(out, `homedeck_probe_http_status_code{service_id="svc_b"`) {
		t.Error("у TCP-проверки не должно быть http_status_code")
	}

	// Удалённая проверка исчезает из метрик и статусов.
	s.Sync([]model.Check{a})
	if _, ok := s.Status("b"); ok {
		t.Error("статус удалённой проверки остался")
	}
	buf.Reset()
	s.WriteMetrics(&buf)
	if strings.Contains(buf.String(), "svc_b") {
		t.Error("метрики удалённой проверки остались")
	}

	// Смена цели сбрасывает накопленное состояние.
	a2 := a
	a2.Revision, a2.Target = 2, "http://other"
	s.Sync([]model.Check{a2})
	if st, _ := s.Status("a"); st.LastRun != nil && st.State == model.StateUp {
		t.Errorf("после смены цели состояние должно начаться заново: %+v", st)
	}
}

func waitRuns(t *testing.T, s *Scheduler, id string, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		runs := 0
		if tr, ok := s.trackers[id]; ok {
			runs = tr.runs
		}
		s.mu.Unlock()
		if runs >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("проверка %s не выполнилась %d раз", id, n)
}

func TestNetProberHTTP(t *testing.T) {
	// httptest слушает loopback: политика должна отклонить соединение до отправки запроса.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()
	c := check("x")
	c.Target = srv.URL
	r := NetProber{UserAgent: "test"}.Probe(context.Background(), c)
	if r.OK || r.Kind != KindForbidden {
		t.Fatalf("loopback должен быть запрещён: %+v", r)
	}
}

func TestClassify(t *testing.T) {
	_, err := net.DefaultResolver.LookupHost(context.Background(), "nonexistent.invalid")
	if kind, _ := Classify(err); kind != KindDNS {
		t.Errorf("DNS: %s", kind)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	if kind, _ := Classify(ctx.Err()); kind != KindTimeout {
		t.Errorf("timeout: %s", kind)
	}
}

// Пока состояние не подтверждено, проверка повторяется через confirmInterval,
// после подтверждения — через обычный интервал.
func TestSchedulerConfirmsQuickly(t *testing.T) {
	old := confirmInterval
	confirmInterval = 20 * time.Millisecond
	defer func() { confirmInterval = old }()
	fp := &fakeProber{}
	s := NewScheduler(fp, slog.New(slog.DiscardHandler))
	defer s.Stop()
	c := check("a")
	c.IntervalS = 3600
	s.Sync([]model.Check{c})

	wait := func(state string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if st, _ := s.Status(c.ID); st.State == state {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		st, _ := s.Status(c.ID)
		t.Fatalf("ожидалось %s, сейчас %+v", state, st)
	}
	wait(model.StateUp)
	calls := fp.calls.Load()
	time.Sleep(150 * time.Millisecond)
	if fp.calls.Load() != calls {
		t.Fatalf("после подтверждения проверка должна ждать обычный интервал: %d → %d", calls, fp.calls.Load())
	}
	if UpThreshold != 2 || calls != UpThreshold {
		t.Fatalf("до зелёного %d проверок", calls)
	}
}
