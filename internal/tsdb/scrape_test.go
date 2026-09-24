package tsdb

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/secrets"
	"go.yaml.in/yaml/v3"
)

func src(id, url string, mod func(*model.SourceInput)) model.Source {
	in := model.SourceInput{Name: id, Kind: model.SourcePrometheus, URL: url}
	if mod != nil {
		mod(&in)
	}
	in.Normalize()
	return model.Source{ID: id, SourceInput: in}
}

func buildParsed(t *testing.T, in ScrapeInput) (scrapeFile, []RuntimeFile) {
	t.Helper()
	out, files, err := BuildScrapeConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	var f scrapeFile
	if err := yaml.Unmarshal(out, &f); err != nil {
		t.Fatal(err)
	}
	return f, files
}

func TestBuildScrapeConfig(t *testing.T) {
	sec1, sec2 := "sec_basic", "sec_bearer"
	in := ScrapeInput{
		InternalListen: "127.0.0.1:9091", VMListen: "127.0.0.1:8428", EgressProxy: "127.0.0.1:9092", RuntimeDir: "/tmp/hd",
		Secrets: map[string]secrets.Payload{sec1: {Username: "u", Password: "p"}, sec2: {Token: "tkn"}},
		Sources: []model.Source{
			src("src_b", "https://nas:9100/custom/path?module=http_2xx&target=x", func(s *model.SourceInput) {
				s.SecretID = &sec1
				s.Labels = map[string]string{"source_id": "evil", "room": "hall"}
				s.TLS.ServerName = "nas.lan"
			}),
			src("src_a", "http://router:9100", func(s *model.SourceInput) { s.SecretID = &sec2; s.IntervalS, s.TimeoutS = 60, 10 }),
			src("src_off", "http://off:9100/metrics", func(s *model.SourceInput) { s.Enabled = new(false) }),
		},
	}
	f, files := buildParsed(t, in)
	names := []string{}
	for _, j := range f.ScrapeConfigs {
		names = append(names, j.JobName)
	}
	// Системные job'ы всегда первые, пользовательские отсортированы, выключенный не попадает.
	if strings.Join(names, ",") != "homedeck,tsdb,src_a,src_b" {
		t.Fatalf("jobs: %v", names)
	}
	for _, j := range f.ScrapeConfigs[:2] {
		if j.ProxyURL != "" || j.StaticConfigs[0].Labels["source_id"] != j.JobName {
			t.Errorf("системный job %s: %+v", j.JobName, j)
		}
	}
	a, b := f.ScrapeConfigs[2], f.ScrapeConfigs[3]
	if a.MetricsPath != "/metrics" || a.ScrapeInterval != "60s" || a.ScrapeTimeout != "10s" {
		t.Errorf("src_a: %+v", a)
	}
	if a.Authorization == nil || a.Authorization.Type != "Bearer" || a.Authorization.CredentialsFile != "/tmp/hd/secrets/src_a/token" || a.BasicAuth != nil {
		t.Errorf("bearer: %+v", a.Authorization)
	}
	// Пользовательская метка source_id не должна переопределять служебную.
	if b.StaticConfigs[0].Labels["source_id"] != "src_b" || b.StaticConfigs[0].Labels["room"] != "hall" {
		t.Errorf("labels: %v", b.StaticConfigs[0].Labels)
	}
	if b.Scheme != "https" || b.MetricsPath != "/custom/path" || b.Params["module"][0] != "http_2xx" || b.StaticConfigs[0].Targets[0] != "nas:9100" {
		t.Errorf("url разобран неверно: %+v", b)
	}
	if b.ProxyURL != "http://127.0.0.1:9092" || b.FollowRedirects || b.HonorLabels || b.SampleLimit != SampleLimit || b.SeriesLimit != SeriesPerTarget {
		t.Errorf("ограничения пользовательского job: %+v", b)
	}
	if b.BasicAuth == nil || b.BasicAuth.Username != "u" || b.BasicAuth.PasswordFile != "/tmp/hd/secrets/src_b/password" {
		t.Errorf("basic: %+v", b.BasicAuth)
	}
	if b.TLSConfig == nil || b.TLSConfig.ServerName != "nas.lan" {
		t.Errorf("tls: %+v", b.TLSConfig)
	}
	raw, _, _ := BuildScrapeConfig(in)
	// Значения секретов живут только в runtime-файлах, не в конфиге.
	if strings.Contains(string(raw), "tkn") || strings.Contains(string(raw), ": p\n") {
		t.Error("секрет попал в scrape-конфиг")
	}
	got := map[string]string{}
	for _, rf := range files {
		got[rf.Path] = string(rf.Content)
	}
	if got["/tmp/hd/secrets/src_a/token"] != "tkn" || got["/tmp/hd/secrets/src_b/password"] != "p" {
		t.Errorf("runtime-файлы: %v", got)
	}

	in.Sources = []model.Source{src("src_x", "http://x:1/m", func(s *model.SourceInput) { s.SecretID = new("sec_missing") })}
	if _, _, err := BuildScrapeConfig(in); err == nil {
		t.Error("недоступный секрет должен давать ошибку")
	}
}

func fakeBinary(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vm")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSupervisorGivesUp(t *testing.T) {
	s := NewSupervisor(Options{Binary: fakeBinary(t, "exit 1"), Listen: "127.0.0.1:1"}, slog.New(slog.DiscardHandler))
	s.backoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond}
	err := s.Run(context.Background())
	if !errors.Is(err, ErrGaveUp) {
		t.Fatalf("ожидался ErrGaveUp: %v", err)
	}
	st := s.Status()
	// 5 неудачных запусков: 4 перезапуска и остановка с ошибкой, дальше рестарт делает Docker.
	if st.State != StateFailed || st.Restarts != 4 || !strings.Contains(st.Error, "кодом 1") {
		t.Fatalf("статус: %+v", st)
	}
}

func TestSupervisorLifecycle(t *testing.T) {
	s := NewSupervisor(Options{Binary: fakeBinary(t, "exec sleep 30"), Listen: "127.0.0.1:1"}, slog.New(slog.DiscardHandler))
	s.health = func(context.Context) error { return nil }
	ready := make(chan struct{})
	s.OnReady(func() { close(ready) })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("OnReady не вызван")
	}
	if !s.Ready() {
		t.Fatal("после health ok состояние должно быть running")
	}
	start := time.Now()
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// SIGTERM должен завершить процесс сразу, без ожидания 45-секундного kill.
	if time.Since(start) > 5*time.Second || s.Status().State != StateStopped {
		t.Fatalf("остановка: %s, %+v", time.Since(start), s.Status())
	}
}

func TestSupervisorArgs(t *testing.T) {
	args := strings.Join(Options{Listen: "127.0.0.1:8428", Retention: "30d", SeriesPerTarget: 20000, MemoryPercent: 40, MaxScrapeSize: "16MiB"}.Args(), " ")
	for _, want := range []string{"-httpListenAddr=127.0.0.1:8428", "-retentionPeriod=30d", "-search.maxResponseSeries=100", "-search.maxQueryDuration=5s", "-promscrape.config.strictParse"} {
		if !strings.Contains(args, want) {
			t.Errorf("нет флага %s", want)
		}
	}
}
