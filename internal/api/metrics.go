package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/netguard"
	"github.com/fess932/homeLab/internal/probe"
	"github.com/fess932/homeLab/internal/tsdb"
)

const perClientQueries = 4

type exprSource struct {
	Query    string            `json:"query"`
	PresetID string            `json:"preset_id"`
	Vars     map[string]string `json:"vars"`
}

func (s *Server) resolve(r *http.Request, in exprSource, allowRaw bool) (string, model.Preset, error) {
	if in.PresetID != "" {
		p, err := s.preset(r, in.PresetID)
		if err != nil {
			return "", p, model.Invalid("preset_id", "шаблон не найден")
		}
		if err := model.ValidateVars(in.Vars); err != nil {
			return "", p, err
		}
		expr, err := model.ExpandPreset(p.Expression, in.Vars)
		return expr, p, err
	}
	q := strings.TrimSpace(in.Query)
	if !allowRaw || q == "" {
		return "", model.Preset{}, model.Invalid("query", "укажите выражение или шаблон")
	}
	if len(q) > 4000 {
		return "", model.Preset{}, model.Invalid("query", "до 4000 символов")
	}
	return q, model.Preset{}, nil
}

func (s *Server) minStep(r *http.Request, p model.Preset, vars map[string]string) time.Duration {
	step := tsdb.DefaultMinStep
	if p.MinStepS > 0 {
		step = max(step, time.Duration(p.MinStepS)*time.Second)
	}
	if id := vars["source_id"]; id != "" && !isSystemSource(id) {
		if src, err := s.Store.GetSource(r.Context(), id); err == nil {
			step = max(step, time.Duration(src.IntervalS)*time.Second)
		}
	}
	if id := vars["service_id"]; id != "" {
		if sv, err := s.Store.GetService(r.Context(), id); err == nil && sv.CheckID != nil {
			if c, err := s.Store.GetCheck(r.Context(), *sv.CheckID); err == nil {
				step = max(step, time.Duration(c.IntervalS)*time.Second)
			}
		}
	}
	return step
}

func (s *Server) acquire(r *http.Request, key string) (func(), error) {
	v, _ := s.perClient.LoadOrStore(key, make(chan struct{}, perClientQueries))
	sem := v.(chan struct{})
	ctx, cancel := context.WithTimeout(r.Context(), tsdb.QueryTimeout)
	defer cancel()
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		if r.Context().Err() != nil {
			return nil, r.Context().Err()
		}
		return nil, &Error{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: "слишком много одновременных запросов графиков"}
	}
}

func (s *Server) clientKey(r *http.Request) string {
	if sess := currentSession(r); sess != nil {
		return "s:" + hex.EncodeToString(sess.tokenHash[:8])
	}
	return "ip:" + s.clientIP(r)
}

func (s *Server) tsdbReady() error {
	if !s.Supervisor.Ready() {
		st := s.Supervisor.Status()
		msg := "мониторинг запускается"
		if st.State != tsdb.StateStarting {
			msg = "хранилище метрик недоступно: " + st.State
		}
		return &Error{Status: http.StatusServiceUnavailable, Code: "tsdb_unavailable", Message: msg}
	}
	return nil
}

func label(res *tsdb.RangeResult, p model.Preset) {
	res.Unit = p.Unit
	for i := range res.Series {
		res.Series[i].Name = model.LegendName(p.Legend, res.Series[i].Labels)
	}
}

func labelInstant(res *tsdb.InstantResult, p model.Preset) {
	res.Unit = p.Unit
	for i := range res.Samples {
		res.Samples[i].Name = model.LegendName(p.Legend, res.Samples[i].Labels)
	}
}

func (s *Server) query(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		exprSource
		Time *model.Time `json:"time"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	expr, p, err := s.resolve(r, in.exprSource, true)
	if err != nil {
		return err
	}
	res, err := s.instant(r, expr, p, in.Time)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}

func (s *Server) instant(r *http.Request, expr string, p model.Preset, at *model.Time) (tsdb.InstantResult, error) {
	if err := s.tsdbReady(); err != nil {
		return tsdb.InstantResult{}, err
	}
	release, err := s.acquire(r, s.clientKey(r))
	if err != nil {
		return tsdb.InstantResult{}, err
	}
	defer release()
	t := time.Now()
	if at != nil {
		t = at.Time
	}
	res, err := s.TSDB.Query(r.Context(), expr, t)
	if err != nil {
		return res, err
	}
	labelInstant(&res, p)
	return res, nil
}

func (s *Server) queryRange(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		exprSource
		Start  model.Time `json:"start"`
		End    model.Time `json:"end"`
		Points int        `json:"points"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	expr, p, err := s.resolve(r, in.exprSource, true)
	if err != nil {
		return err
	}
	res, err := s.rangeQuery(r, expr, p, in.Vars, in.Start.Time, in.End.Time, in.Points)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}

func (s *Server) retention() time.Duration {
	v := s.Config.Retention
	n, _ := strconv.Atoi(v[:len(v)-1])
	unit := map[byte]time.Duration{'h': time.Hour, 'd': 24 * time.Hour, 'w': 7 * 24 * time.Hour, 'y': 365 * 24 * time.Hour}[v[len(v)-1]]
	return time.Duration(n) * unit
}

func (s *Server) rangeQuery(r *http.Request, expr string, p model.Preset, vars map[string]string, start, end time.Time, points int) (tsdb.RangeResult, error) {
	now := time.Now()
	if start.IsZero() || end.IsZero() {
		return tsdb.RangeResult{}, model.Invalid("start", "укажите start и end")
	}
	if end.After(now) {
		end = now
	}
	if oldest := now.Add(-s.retention() - 24*time.Hour); start.Before(oldest) {
		start = oldest
	}
	if !start.Before(end) {
		return tsdb.RangeResult{}, model.Invalid("start", "начало диапазона должно быть раньше конца и внутри хранения")
	}
	if points == 0 {
		points = 300
	}
	if err := s.tsdbReady(); err != nil {
		return tsdb.RangeResult{}, err
	}
	release, err := s.acquire(r, s.clientKey(r))
	if err != nil {
		return tsdb.RangeResult{}, err
	}
	defer release()
	gs, ge, step := tsdb.Grid(start, end, points, s.minStep(r, p, vars))
	res, err := s.TSDB.QueryRange(r.Context(), expr, gs, ge, step)
	if err != nil {
		return res, err
	}
	label(&res, p)
	return res, nil
}

type widgetData struct {
	Range      *tsdb.RangeResult   `json:"range,omitempty"`
	Instant    *tsdb.InstantResult `json:"instant,omitempty"`
	Status     *model.CheckStatus  `json:"status,omitempty"`
	Thresholds []model.Threshold   `json:"thresholds"`
}

func (s *Server) widgetData(w http.ResponseWriter, r *http.Request) error {
	wg, _, err := s.Store.Widget(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	q := r.URL.Query()
	var start, end time.Time
	if q.Get("start") != "" || q.Get("end") != "" {
		if start, err = time.Parse(time.RFC3339, q.Get("start")); err != nil {
			return model.Invalid("start", "RFC 3339")
		}
		if end, err = time.Parse(time.RFC3339, q.Get("end")); err != nil {
			return model.Invalid("end", "RFC 3339")
		}
	}
	data, err := s.buildWidgetData(r, wg, q.Get("range"), start, end, atoi(q.Get("points")))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, data)
	return nil
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func (s *Server) buildWidgetData(r *http.Request, wg model.Widget, rangeName string, start, end time.Time, points int) (widgetData, error) {
	data := widgetData{Thresholds: []model.Threshold{}}
	for _, sid := range wg.ServiceIDs() {
		if wg.Type == model.WidgetLinks {
			break
		}
		sv, err := s.Store.GetService(r.Context(), sid)
		if err != nil {
			return data, err
		}
		s.withStatus(&sv)
		data.Status = sv.Status
	}
	ref := wg.MetricRef()
	if ref == nil || ref.PresetID == "" {
		return data, nil
	}
	p, err := s.preset(r, ref.PresetID)
	if err != nil {
		return data, model.Invalid("preset_id", "шаблон виджета не найден")
	}
	data.Thresholds = p.Thresholds
	expr, err := model.ExpandPreset(p.Expression, ref.Vars)
	if err != nil {
		return data, err
	}
	if wg.Type == model.WidgetNumber || wg.Type == model.WidgetLink {
		inst, err := s.instant(r, expr, p, nil)
		if err != nil {
			return data, err
		}
		data.Instant = &inst
		if wg.Type == model.WidgetNumber {
			return data, nil
		}
	}
	if start.IsZero() {
		if rangeName == "" {
			rangeName = wg.ChartRange()
		}
		if rangeName == "" {
			rangeName = "1h"
		}
		d, ok := model.RangeDuration(rangeName)
		if !ok {
			return data, model.Invalid("range", "1h, 6h, 24h, 7d или 30d")
		}
		end = time.Now()
		start = end.Add(-d)
	}
	if points == 0 && wg.Type == model.WidgetLink {
		points = 60
	}
	rr, err := s.rangeQuery(r, expr, p, ref.Vars, start, end, points)
	if err != nil {
		return data, err
	}
	data.Range = &rr
	return data, nil
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	type counts struct {
		Total    int `json:"total"`
		Up       int `json:"up"`
		Down     int `json:"down"`
		Stale    int `json:"stale,omitempty"`
		Unknown  int `json:"unknown,omitempty"`
		Pending  int `json:"pending,omitempty"`
		Disabled int `json:"disabled"`
	}
	var out struct {
		Now  model.Time `json:"now"`
		TSDB struct {
			State    string      `json:"state"`
			Restarts int         `json:"restarts"`
			Error    string      `json:"error"`
			Since    *model.Time `json:"since"`
		} `json:"tsdb"`
		Config struct {
			Desired int64  `json:"desired_revision"`
			Applied int64  `json:"applied_revision"`
			Error   string `json:"error"`
		} `json:"config"`
		Disk struct {
			Total   int64  `json:"total_bytes"`
			Free    int64  `json:"free_bytes"`
			Metrics int64  `json:"metrics_bytes"`
			App     int64  `json:"app_bytes"`
			Level   string `json:"level"`
		} `json:"disk"`
		Services counts `json:"services"`
		Sources  counts `json:"sources"`
	}
	out.Now = model.Time{Time: time.Now().UTC()}
	st := s.Supervisor.Status()
	out.TSDB.State, out.TSDB.Restarts, out.TSDB.Error, out.TSDB.Since = st.State, st.Restarts, st.Error, model.TimePtr(st.Since)
	cs, err := s.Store.ConfigState(ctx)
	if err != nil {
		return err
	}
	out.Config.Desired, out.Config.Applied, out.Config.Error = cs.Desired, cs.Applied, cs.Error
	d := s.Disk()
	out.Disk.Total, out.Disk.Free, out.Disk.Metrics, out.Disk.App = d.Total, d.Free, d.Metrics, d.App
	out.Disk.Level = DiskLevel(d)

	services, err := s.Store.ListServices(ctx)
	if err != nil {
		return err
	}
	for _, sv := range services {
		if sv.CheckID == nil {
			continue
		}
		out.Services.Total++
		st, _ := s.Scheduler.Status(*sv.CheckID)
		switch st.State {
		case model.StateUp:
			out.Services.Up++
		case model.StateDown:
			out.Services.Down++
		case model.StateStale:
			out.Services.Stale++
		case model.StateDisabled:
			out.Services.Disabled++
		default:
			out.Services.Unknown++
		}
	}
	sources, err := s.Store.ListSources(ctx)
	if err != nil {
		return err
	}
	for _, src := range append(systemSources(), sources...) {
		out.Sources.Total++
		switch s.Watcher.Status(src).State {
		case model.StateUp:
			out.Sources.Up++
		case model.StateDown:
			out.Sources.Down++
		case "disabled":
			out.Sources.Disabled++
		default:
			out.Sources.Pending++
		}
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func DiskLevel(d DiskUsage) string {
	if d.Total <= 0 {
		return "ok"
	}
	ratio := float64(d.Free) / float64(d.Total)
	switch {
	case ratio < 0.10 || d.Free < 1<<30:
		return "critical"
	case ratio < 0.20:
		return "warning"
	}
	return "ok"
}

type sourceTestResult struct {
	OK         bool    `json:"ok"`
	HTTPStatus *int    `json:"http_status"`
	DurationMS float64 `json:"duration_ms"`
	Samples    int     `json:"samples"`
	Bytes      int     `json:"bytes"`
	ErrorKind  string  `json:"error_kind"`
	Error      string  `json:"error"`
}

func (s *Server) testSource(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if isSystemSource(id) {
		return &Error{Status: http.StatusForbidden, Code: "forbidden", Message: "встроенный источник проверяется автоматически"}
	}
	src, err := s.Store.GetSource(r.Context(), id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, s.scrapeOnce(r.Context(), src.SourceInput))
	return nil
}

func (s *Server) testNewSource(w http.ResponseWriter, r *http.Request) error {
	var in model.SourceInput
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Normalize()
	if err := in.Validate(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, s.scrapeOnce(r.Context(), in))
	return nil
}

func (s *Server) scrapeOnce(ctx context.Context, in model.SourceInput) sourceTestResult {
	var res sourceTestResult
	fail := func(kind, msg string) sourceTestResult {
		res.ErrorKind, res.Error = kind, msg
		return res
	}
	tlsCfg, err := probe.TLSConfig(in.TLS.CAPEM, in.TLS.ServerName)
	if err != nil {
		return fail(probe.KindTLS, err.Error())
	}
	tr := netguard.Transport(tlsCfg)
	tr.DisableKeepAlives = true
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, CheckRedirect: netguard.NoRedirects}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(in.TimeoutS)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, in.URL, nil)
	if err != nil {
		return fail(probe.KindConnect, err.Error())
	}
	req.Header.Set("Accept", "text/plain;version=0.0.4;q=1,*/*;q=0.1")
	req.Header.Set("User-Agent", "HomeDeck/"+s.Config.Version)
	if in.SecretID != nil {
		rec, err := s.Store.GetSecret(ctx, *in.SecretID)
		if err != nil {
			return fail("", "секрет не найден")
		}
		p, err := s.Box.Open(rec.ID, rec.Payload)
		if err != nil {
			return fail("", err.Error())
		}
		if p.Token != "" {
			req.Header.Set("Authorization", "Bearer "+p.Token)
		} else {
			req.SetBasicAuth(p.Username, p.Password)
		}
	}
	start := time.Now()
	resp, err := client.Do(req)
	res.DurationMS = float64(time.Since(start).Microseconds()) / 1000
	if err != nil {
		kind, msg := probe.Classify(err)
		return fail(kind, msg)
	}
	defer resp.Body.Close()
	res.HTTPStatus = &resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("код ответа %d", resp.StatusCode)
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			msg += ": перенаправления запрещены, укажите конечный адрес"
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			msg += ": проверьте секрет"
		}
		return fail(probe.KindHTTPStatus, msg)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, tsdb.MaxResponseBytes+1))
	res.DurationMS = float64(time.Since(start).Microseconds()) / 1000
	if err != nil {
		kind, msg := probe.Classify(err)
		return fail(kind, msg)
	}
	res.Bytes = len(body)
	if len(body) > tsdb.MaxResponseBytes {
		return fail("too_large", "ответ больше 10 MiB")
	}
	samples, bad := countSamples(body)
	res.Samples = samples
	switch {
	case bad != "":
		return fail("parse", "ответ не похож на формат Prometheus: "+bad)
	case samples == 0:
		return fail("parse", "ответ не содержит метрик")
	case samples > tsdb.SampleLimit:
		return fail("too_many_samples", fmt.Sprintf("%d samples при лимите %d на scrape; источник будет отклоняться", samples, tsdb.SampleLimit))
	}
	res.OK = true
	return res
}

func countSamples(body []byte) (int, string) {
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	n := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name := line
		if i := strings.IndexAny(line, "{ "); i > 0 {
			name = line[:i]
		}
		if !validMetricName(name) || !strings.ContainsAny(line, " \t") {
			if len(line) > 80 {
				line = line[:80]
			}
			return n, line
		}
		n++
	}
	if err := sc.Err(); err != nil {
		return n, err.Error()
	}
	return n, ""
}

func validMetricName(s string) bool {
	for i, c := range s {
		ok := c == '_' || c == ':' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (i > 0 && c >= '0' && c <= '9')
		if !ok {
			return false
		}
	}
	return s != ""
}
