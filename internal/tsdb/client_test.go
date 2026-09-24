package tsdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGrid(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 34, 56, 0, time.UTC)
	cases := []struct {
		span    time.Duration
		points  int
		minStep time.Duration
	}{
		{time.Hour, 300, 0},
		{time.Hour, 300, time.Minute},
		{6 * time.Hour, 300, 0},
		{24 * time.Hour, 1000, 0},
		{7 * 24 * time.Hour, 500, 0},
		{30 * 24 * time.Hour, 1000, 0},
		{30 * 24 * time.Hour, 10, 0},
		{365 * 24 * time.Hour, 1000, 0},
		{time.Second, 5, 0},
		{time.Hour, 5000, 0},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%s/%d/%s", c.span, c.points, c.minStep), func(t *testing.T) {
			s, e, step := Grid(now.Add(-c.span), now, c.points, c.minStep)
			n := int(e.Sub(s)/step) + 1
			if n > MaxPoints {
				t.Fatalf("точек %d > %d", n, MaxPoints)
			}
			// Шаг не меньше интервала сбора: иначе график рисует «пустые» точки между scrape.
			if step < max(c.minStep, DefaultMinStep) {
				t.Fatalf("шаг %s меньше минимального", step)
			}
			// Границы кратны шагу: одинаковые запросы с разных вкладок попадают в кеш.
			if s.Unix()%int64(step.Seconds()) != 0 || e.Unix()%int64(step.Seconds()) != 0 {
				t.Fatalf("границы не выровнены: %s %s шаг %s", s, e, step)
			}
			if s.After(now.Add(-c.span)) || e.Before(now) {
				t.Fatalf("сетка не покрывает диапазон: %s..%s", s, e)
			}
			if c.span <= 30*24*time.Hour && step <= 24*time.Hour && !slices.Contains(niceSteps, step) {
				t.Fatalf("шаг %s не из списка «красивых»", step)
			}
		})
	}
}

func TestToRange(t *testing.T) {
	start := time.Unix(1000, 0)
	end := time.Unix(1060, 0)
	body := promResponse{Status: "success", Data: json.RawMessage(`{"resultType":"matrix","result":[
		{"metric":{"a":"1"},"values":[[1000,"1"],[1015,"NaN"],[1045,"+Inf"],[1060,"2.5"]]},
		{"metric":{},"values":[[990,"7"],[1030,"-Inf"],[2000,"9"]]}
	]}`)}
	res, err := toRange(body, start, end, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res.Start != 1000 || res.Step != 15 || len(res.Series) != 2 {
		t.Fatalf("%+v", res)
	}
	// NaN, ±Inf и отсутствующие точки — null, а не ноль: UI рисует разрыв.
	want := []*float64{new(1.0), nil, nil, nil, new(2.5)}
	for i, v := range res.Series[0].Values {
		if (v == nil) != (want[i] == nil) || (v != nil && *v != *want[i]) {
			t.Errorf("точка %d: %v, ожидалось %v", i, v, want[i])
		}
	}
	for i, v := range res.Series[1].Values {
		if v != nil {
			t.Errorf("точки вне сетки и -Inf должны отбрасываться, индекс %d = %v", i, *v)
		}
	}
	if res.Series[1].Labels == nil || res.Warnings == nil {
		t.Error("пустые labels/warnings должны сериализоваться как {} и []")
	}

	many := `{"resultType":"matrix","result":[` + strings.TrimSuffix(strings.Repeat(`{"metric":{},"values":[]},`, MaxSeries+1), ",") + `]}`
	if _, err := toRange(promResponse{Status: "success", Data: json.RawMessage(many)}, start, end, 15*time.Second); !isCode(err, CodeTooMany) {
		t.Fatalf("лимит рядов: %v", err)
	}
	if _, err := toRange(promResponse{Status: "success", Data: json.RawMessage(`{"resultType":"vector","result":[]}`)}, start, end, 15*time.Second); !isCode(err, CodeBadQuery) {
		t.Fatalf("vector для range: %v", err)
	}
}

func TestToInstant(t *testing.T) {
	res, err := toInstant(promResponse{Status: "success", Data: json.RawMessage(`{"resultType":"vector","result":[
		{"metric":{"a":"1"},"value":[1000,"3"]},{"metric":{"a":"2"},"value":[1000,"NaN"]}]}`)})
	if err != nil || len(res.Samples) != 2 || *res.Samples[0].Value != 3 || res.Samples[1].Value != nil || res.Samples[0].Time != 1000 {
		t.Fatalf("%+v %v", res, err)
	}
	res, err = toInstant(promResponse{Status: "success", Data: json.RawMessage(`{"resultType":"scalar","result":[1000,"42"]}`)})
	if err != nil || len(res.Samples) != 1 || *res.Samples[0].Value != 42 {
		t.Fatalf("scalar: %+v %v", res, err)
	}
	if _, err := toInstant(promResponse{Status: "success", Data: json.RawMessage(`{"resultType":"matrix","result":[]}`)}); !isCode(err, CodeBadQuery) {
		t.Fatalf("matrix для instant: %v", err)
	}
}

func isCode(err error, code string) bool {
	qe, ok := errors.AsType[*QueryError](err)
	return ok && qe.Code == code
}

type fakeVM struct {
	calls    atomic.Int64
	release  chan struct{}
	canceled atomic.Int64
	started  chan struct{}
}

func newFakeVM(t *testing.T) (*fakeVM, *Client) {
	fv := &fakeVM{release: make(chan struct{}), started: make(chan struct{}, 10)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fv.calls.Add(1)
		fv.started <- struct{}{}
		select {
		case <-fv.release:
		case <-r.Context().Done():
			fv.canceled.Add(1)
			return
		}
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"1"]}]}}`)
	}))
	t.Cleanup(srv.Close)
	return fv, NewClient(strings.TrimPrefix(srv.URL, "http://"))
}

func TestSharedDedup(t *testing.T) {
	fv, c := newFakeVM(t)
	at := time.Unix(1000, 0)
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if _, err := c.Query(context.Background(), "up", at); err != nil {
				t.Error(err)
			}
		})
	}
	<-fv.started
	// Даём остальным вызовам присоединиться к уже идущему запросу.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		w := 0
		for _, cl := range c.inflight {
			w = cl.waiters
		}
		c.mu.Unlock()
		if w == 5 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(fv.release)
	wg.Wait()
	if n := fv.calls.Load(); n != 1 {
		t.Fatalf("одинаковые запросы должны объединяться: %d обращений", n)
	}

	// Повтор в пределах TTL берётся из кеша, после TTL — снова в TSDB.
	if _, err := c.Query(context.Background(), "up", at); err != nil {
		t.Fatal(err)
	}
	if fv.calls.Load() != 1 {
		t.Fatal("кеш не сработал")
	}
	c.now = func() time.Time { return time.Now().Add(CacheTTL + time.Second) }
	if _, err := c.Query(context.Background(), "up", at); err != nil {
		t.Fatal(err)
	}
	if fv.calls.Load() != 2 {
		t.Fatal("после TTL запрос должен идти в TSDB")
	}
}

func TestSharedCancel(t *testing.T) {
	fv, c := newFakeVM(t)
	// Единственный ожидающий ушёл — upstream-запрос отменяется.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := c.Query(ctx, "a", time.Unix(1, 0)); done <- err }()
	<-fv.started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("ожидалась отмена: %v", err)
	}
	waitFor(t, func() bool { return fv.canceled.Load() == 1 })

	// Один из двух ушёл — второй получает результат, upstream не отменяется.
	ctx2, cancel2 := context.WithCancel(context.Background())
	res := make(chan error, 2)
	go func() { _, err := c.Query(ctx2, "b", time.Unix(1, 0)); res <- err }()
	<-fv.started
	go func() { _, err := c.Query(context.Background(), "b", time.Unix(1, 0)); res <- err }()
	waitFor(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, cl := range c.inflight {
			if cl.waiters == 2 {
				return true
			}
		}
		return false
	})
	cancel2()
	if err := <-res; !errors.Is(err, context.Canceled) {
		t.Fatalf("первый: %v", err)
	}
	close(fv.release)
	if err := <-res; err != nil {
		t.Fatalf("второй должен получить результат: %v", err)
	}
	if fv.canceled.Load() != 1 {
		t.Fatal("upstream отменён, хотя оставался ожидающий")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("условие не выполнилось")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		body   string
		code   string
	}{
		{422, `{"status":"error","errorType":"422","error":"cannot parse \"foo{\": unexpected end"}`, CodeBadQuery},
		// Имя метрики со словом timeout в тексте ошибки не должно превращаться в query_timeout.
		{422, `{"status":"error","error":"error when executing query=\"http_timeout_total{\": cannot parse"}`, CodeBadQuery},
		{422, `{"status":"error","error":"the number of matching timeseries exceeds 100; either narrow down the search or increase -search.maxResponseSeries"}`, CodeTooMany},
		{503, `{"status":"error","error":"cannot execute query in 5s: deadline exceeded; increase -search.maxQueryDuration"}`, CodeTimeout},
		{503, `{"status":"error","error":"too many concurrent requests"}`, CodeUnavailable},
		{500, `oops`, CodeUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.body[:min(40, len(tc.body))], func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			c := NewClient(strings.TrimPrefix(srv.URL, "http://"))
			_, err := c.Query(context.Background(), "x", time.Unix(1, 0))
			if !isCode(err, tc.code) {
				t.Fatalf("ожидался %s, получено %v", tc.code, err)
			}
		})
	}
	c := NewClient("127.0.0.1:1")
	if _, err := c.Query(context.Background(), "x", time.Unix(1, 0)); !isCode(err, CodeUnavailable) {
		t.Fatalf("недоступная TSDB: %v", err)
	}
}

func TestResponseTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[`)
		chunk := strings.Repeat(`{"metric":{"a":"xxxxxxxxxxxxxxxx"},"value":[1,"1"]},`, 1000)
		for range (MaxResponseBytes / len(chunk)) + 2 {
			fmt.Fprint(w, chunk)
		}
		fmt.Fprint(w, `]}}`)
	}))
	defer srv.Close()
	c := NewClient(strings.TrimPrefix(srv.URL, "http://"))
	if _, err := c.Query(context.Background(), "x", time.Unix(1, 0)); !isCode(err, CodeTooLarge) {
		t.Fatalf("ожидался payload_too_large: %v", err)
	}
}

func TestStats(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/series/count":
			fmt.Fprint(w, `{"status":"success","data":[916]}`)
		case "/metrics":
			fmt.Fprint(w, "vm_rows{type=\"storage/inmemory\"} 915\nvm_rows{type=\"storage/small\"} 100\nvm_rows{type=\"indexdb/inmemory\"} 7954\n")
		}
	}))
	defer srv.Close()
	st, err := NewClient(strings.TrimPrefix(srv.URL, "http://")).Stats(context.Background())
	if err != nil || st.Series != 916 || st.Samples != 1015 {
		t.Fatalf("Stats: %+v %v", st, err)
	}
}
