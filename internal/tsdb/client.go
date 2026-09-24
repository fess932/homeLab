package tsdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	MaxSeries         = 100
	MaxPoints         = 1000
	MaxResponseBytes  = 10 << 20
	QueryTimeout      = 5 * time.Second
	CacheTTL          = 5 * time.Second
	MaxUpstream       = 8
	DefaultMinStep    = 15 * time.Second
	queryHardDeadline = QueryTimeout + time.Second
)

type QueryError struct {
	Code    string
	Message string
}

func (e *QueryError) Error() string { return e.Code + ": " + e.Message }

const (
	CodeBadQuery    = "bad_query"
	CodeTooMany     = "too_many_series"
	CodeTimeout     = "query_timeout"
	CodeUnavailable = "tsdb_unavailable"
	CodeTooLarge    = "payload_too_large"
)

type Series struct {
	Labels map[string]string `json:"labels"`
	Name   string            `json:"name"`
	Values []*float64        `json:"values"`
}

type RangeResult struct {
	Start    int64    `json:"start"`
	Step     int64    `json:"step"`
	Unit     string   `json:"unit"`
	Series   []Series `json:"series"`
	Warnings []string `json:"warnings"`
}

type Sample struct {
	Labels map[string]string `json:"labels"`
	Name   string            `json:"name"`
	Value  *float64          `json:"value"`
	Time   int64             `json:"time"`
}

type InstantResult struct {
	Unit     string   `json:"unit"`
	Samples  []Sample `json:"samples"`
	Warnings []string `json:"warnings"`
}

type Target struct {
	SourceID   string
	Health     string
	LastError  string
	LastScrape time.Time
	Duration   time.Duration
	Samples    int
	ScrapeURL  string
}

type Client struct {
	base string
	http *http.Client
	sem  chan struct{}
	now  func() time.Time

	mu       sync.Mutex
	cache    map[string]cacheEntry
	inflight map[string]*call
}

type cacheEntry struct {
	val     any
	expires time.Time
}

type call struct {
	done    chan struct{}
	val     any
	err     error
	waiters int
	cancel  context.CancelFunc
}

func NewClient(addr string) *Client {
	return &Client{
		base:     "http://" + addr,
		http:     &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: MaxUpstream, IdleConnTimeout: time.Minute}},
		sem:      make(chan struct{}, MaxUpstream),
		now:      time.Now,
		cache:    map[string]cacheEntry{},
		inflight: map[string]*call{},
	}
}

var niceSteps = []time.Duration{
	15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute,
	15 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour,
}

func Grid(start, end time.Time, points int, minStep time.Duration) (time.Time, time.Time, time.Duration) {
	points = min(max(points, 10), MaxPoints)
	minStep = max(minStep, DefaultMinStep)
	span := max(end.Sub(start), time.Minute)
	raw := time.Duration(math.Ceil(float64(span) / float64(points-2)))
	step := niceSteps[len(niceSteps)-1]
	for _, s := range niceSteps {
		if s >= raw && s >= minStep {
			step = s
			break
		}
	}
	if raw > step {
		step = raw.Round(time.Second)
	}
	s, e := align(start, end, step)
	for int(e.Sub(s)/step)+1 > MaxPoints {
		step *= 2
		s, e = align(start, end, step)
	}
	return s, e, step
}

func align(start, end time.Time, step time.Duration) (time.Time, time.Time) {
	st := int64(step / time.Second)
	s := start.Unix() / st * st
	e := (end.Unix() + st - 1) / st * st
	return time.Unix(s, 0).UTC(), time.Unix(e, 0).UTC()
}

func (c *Client) QueryRange(ctx context.Context, expr string, start, end time.Time, step time.Duration) (RangeResult, error) {
	key := fmt.Sprintf("r|%s|%d|%d|%d", expr, start.Unix(), end.Unix(), int64(step.Seconds()))
	v, err := c.shared(ctx, key, func(ctx context.Context) (any, error) {
		q := url.Values{"query": {expr}, "start": {strconv.FormatInt(start.Unix(), 10)}, "end": {strconv.FormatInt(end.Unix(), 10)},
			"step": {strconv.FormatInt(int64(step.Seconds()), 10) + "s"}, "timeout": {"5s"}, "nocache": {"0"}}
		var body promResponse
		if err := c.get(ctx, "/api/v1/query_range", q, &body); err != nil {
			return nil, err
		}
		return toRange(body, start, end, step)
	})
	if err != nil {
		return RangeResult{}, err
	}
	return v.(RangeResult), nil
}

func (c *Client) Query(ctx context.Context, expr string, at time.Time) (InstantResult, error) {
	at = at.Truncate(time.Second)
	key := fmt.Sprintf("i|%s|%d", expr, at.Unix())
	v, err := c.shared(ctx, key, func(ctx context.Context) (any, error) {
		q := url.Values{"query": {expr}, "time": {strconv.FormatInt(at.Unix(), 10)}, "timeout": {"5s"}}
		var body promResponse
		if err := c.get(ctx, "/api/v1/query", q, &body); err != nil {
			return nil, err
		}
		return toInstant(body)
	})
	if err != nil {
		return InstantResult{}, err
	}
	return v.(InstantResult), nil
}

func (c *Client) shared(ctx context.Context, key string, fn func(context.Context) (any, error)) (any, error) {
	now := c.now()
	c.mu.Lock()
	if e, ok := c.cache[key]; ok && now.Before(e.expires) {
		c.mu.Unlock()
		return e.val, nil
	}
	if len(c.cache) > 2000 {
		for k, e := range c.cache {
			if !now.Before(e.expires) {
				delete(c.cache, k)
			}
		}
	}
	cl, ok := c.inflight[key]
	if ok {
		cl.waiters++
	} else {
		callCtx, cancel := context.WithTimeout(context.Background(), queryHardDeadline)
		cl = &call{done: make(chan struct{}), waiters: 1, cancel: cancel}
		c.inflight[key] = cl
		go func() {
			defer cancel()
			select {
			case c.sem <- struct{}{}:
				cl.val, cl.err = fn(callCtx)
				<-c.sem
			case <-callCtx.Done():
				cl.err = callCtx.Err()
			}
			c.mu.Lock()
			if c.inflight[key] == cl {
				delete(c.inflight, key)
			}
			if cl.err == nil {
				c.cache[key] = cacheEntry{val: cl.val, expires: c.now().Add(CacheTTL)}
			}
			c.mu.Unlock()
			close(cl.done)
		}()
	}
	c.mu.Unlock()
	select {
	case <-cl.done:
		if errors.Is(cl.err, context.DeadlineExceeded) {
			return nil, &QueryError{CodeTimeout, "запрос не уложился в 5 секунд, сузьте диапазон или уточните выражение"}
		}
		return cl.val, cl.err
	case <-ctx.Done():
		c.mu.Lock()
		cl.waiters--
		if cl.waiters == 0 {
			cl.cancel()
			if c.inflight[key] == cl {
				delete(c.inflight, key)
			}
		}
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

type promResponse struct {
	Status    string          `json:"status"`
	ErrorType string          `json:"errorType"`
	Error     string          `json:"error"`
	Warnings  []string        `json:"warnings"`
	Data      json.RawMessage `json:"data"`
}

type promData struct {
	ResultType string          `json:"resultType"`
	Result     json.RawMessage `json:"result"`
}

type promSeries struct {
	Metric map[string]string `json:"metric"`
	Values [][2]any          `json:"values"`
	Value  *[2]any           `json:"value"`
}

var (
	tooManyRe = regexp.MustCompile(`-search\.max(ResponseSeries|UniqueTimeseries|Series)\b|too many (time ?series|timeseries)|the number of matching timeseries`)
	timeoutRe = regexp.MustCompile(`-search\.maxQueryDuration|deadline exceeded|timeout exceeded|timed out|cannot execute query in`)
)

func (c *Client) get(ctx context.Context, path string, q url.Values, out *promResponse) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return err
		}
		return &QueryError{CodeUnavailable, "TSDB недоступна"}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return &QueryError{CodeUnavailable, "обрыв ответа TSDB"}
	}
	if len(body) > MaxResponseBytes {
		return &QueryError{CodeTooLarge, "ответ больше 10 MiB, уточните выражение"}
	}
	if err := json.Unmarshal(body, out); err != nil || out.Status != "success" {
		msg := strings.TrimSpace(out.Error)
		if msg == "" {
			msg = strings.TrimSpace(string(body))
		}
		switch {
		case resp.StatusCode == http.StatusServiceUnavailable && !timeoutRe.MatchString(msg):
			return &QueryError{CodeUnavailable, "TSDB перегружена или недоступна"}
		case timeoutRe.MatchString(msg):
			return &QueryError{CodeTimeout, "запрос не уложился в 5 секунд, сузьте диапазон или уточните выражение"}
		case tooManyRe.MatchString(msg):
			return &QueryError{CodeTooMany, fmt.Sprintf("запрос возвращает слишком много рядов (лимит %d), добавьте фильтр или агрегацию", MaxSeries)}
		case resp.StatusCode >= 500:
			return &QueryError{CodeUnavailable, "TSDB вернула ошибку " + resp.Status}
		default:
			return &QueryError{CodeBadQuery, cleanVMError(msg)}
		}
	}
	return nil
}

func cleanVMError(msg string) string {
	if i := strings.Index(msg, "cannot parse"); i >= 0 {
		msg = msg[i:]
	}
	if len(msg) > 500 {
		msg = msg[:500]
	}
	return msg
}

func parseValue(v any) *float64 {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return &f
}

func parseTS(v any) float64 {
	f, _ := v.(float64)
	return f
}

func toRange(body promResponse, start, end time.Time, step time.Duration) (RangeResult, error) {
	var d promData
	if err := json.Unmarshal(body.Data, &d); err != nil {
		return RangeResult{}, &QueryError{CodeUnavailable, "некорректный ответ TSDB"}
	}
	if d.ResultType != "matrix" {
		return RangeResult{}, &QueryError{CodeBadQuery, "выражение должно возвращать временные ряды"}
	}
	var result []promSeries
	if err := json.Unmarshal(d.Result, &result); err != nil {
		return RangeResult{}, &QueryError{CodeUnavailable, "некорректный ответ TSDB"}
	}
	if len(result) > MaxSeries {
		return RangeResult{}, &QueryError{CodeTooMany, fmt.Sprintf("запрос возвращает %d рядов при лимите %d", len(result), MaxSeries)}
	}
	n := int(end.Sub(start)/step) + 1
	stepS := step.Seconds()
	res := RangeResult{Start: start.Unix(), Step: int64(stepS), Series: make([]Series, 0, len(result)), Warnings: nonNil(body.Warnings)}
	for _, r := range result {
		vals := make([]*float64, n)
		for _, p := range r.Values {
			i := int(math.Round((parseTS(p[0]) - float64(start.Unix())) / stepS))
			if i >= 0 && i < n {
				vals[i] = parseValue(p[1])
			}
		}
		res.Series = append(res.Series, Series{Labels: nonNilMap(r.Metric), Values: vals})
	}
	return res, nil
}

func toInstant(body promResponse) (InstantResult, error) {
	var d promData
	if err := json.Unmarshal(body.Data, &d); err != nil {
		return InstantResult{}, &QueryError{CodeUnavailable, "некорректный ответ TSDB"}
	}
	res := InstantResult{Samples: []Sample{}, Warnings: nonNil(body.Warnings)}
	switch d.ResultType {
	case "scalar":
		var sc [2]any
		if err := json.Unmarshal(d.Result, &sc); err != nil {
			return res, &QueryError{CodeUnavailable, "некорректный ответ TSDB"}
		}
		res.Samples = append(res.Samples, Sample{Labels: map[string]string{}, Value: parseValue(sc[1]), Time: int64(parseTS(sc[0]))})
		return res, nil
	case "vector":
	default:
		return res, &QueryError{CodeBadQuery, "выражение должно возвращать мгновенные значения"}
	}
	var result []promSeries
	if err := json.Unmarshal(d.Result, &result); err != nil {
		return res, &QueryError{CodeUnavailable, "некорректный ответ TSDB"}
	}
	if len(result) > MaxSeries {
		return res, &QueryError{CodeTooMany, fmt.Sprintf("запрос возвращает %d рядов при лимите %d", len(result), MaxSeries)}
	}
	for _, r := range result {
		if r.Value == nil {
			continue
		}
		res.Samples = append(res.Samples, Sample{Labels: nonNilMap(r.Metric), Value: parseValue(r.Value[1]), Time: int64(parseTS(r.Value[0]))})
	}
	return res, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func (c *Client) Targets(ctx context.Context) ([]Target, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1/targets?state=active", nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Data struct {
			ActiveTargets []struct {
				Labels             map[string]string `json:"labels"`
				ScrapeURL          string            `json:"scrapeUrl"`
				LastError          string            `json:"lastError"`
				LastScrape         time.Time         `json:"lastScrape"`
				LastScrapeDuration float64           `json:"lastScrapeDuration"`
				LastSamplesScraped int               `json:"lastSamplesScraped"`
				Health             string            `json:"health"`
			} `json:"activeTargets"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, MaxResponseBytes)).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]Target, 0, len(body.Data.ActiveTargets))
	for _, t := range body.Data.ActiveTargets {
		last := t.LastScrape
		if last.Year() < 2000 {
			last = time.Time{}
		}
		out = append(out, Target{
			SourceID:   t.Labels["source_id"],
			Health:     t.Health,
			LastError:  t.LastError,
			LastScrape: last,
			Duration:   time.Duration(t.LastScrapeDuration * float64(time.Second)),
			Samples:    t.LastSamplesScraped,
			ScrapeURL:  t.ScrapeURL,
		})
	}
	return out, nil
}

var versionRe = regexp.MustCompile(`vm_app_version\{[^}]*short_version="([^"]+)"`)

func (c *Client) Version(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/metrics", nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if m := versionRe.FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return ""
}

// Stats — размер базы: число рядов и сохранённых точек.
type Stats struct {
	Series  int64
	Samples int64
}

var rowsRe = regexp.MustCompile(`(?m)^vm_rows\{type="storage/[^"]*"\} (\d+)`)

// Stats читает число рядов (/api/v1/series/count) и точек (vm_rows хранилища из /metrics, без индекса).
func (c *Client) Stats(ctx context.Context) (Stats, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var st Stats
	var count struct {
		Data []int64 `json:"data"`
	}
	b, err := c.fetch(ctx, "/api/v1/series/count")
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &count); err != nil {
		return st, err
	}
	if len(count.Data) > 0 {
		st.Series = count.Data[0]
	}
	if b, err = c.fetch(ctx, "/metrics"); err != nil {
		return st, err
	}
	for _, m := range rowsRe.FindAllSubmatch(b, -1) {
		n, _ := strconv.ParseInt(string(m[1]), 10, 64)
		st.Samples += n
	}
	return st, nil
}

func (c *Client) fetch(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}
