package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fess932/homeLab/internal/assets"
	"github.com/fess932/homeLab/internal/config"
	"github.com/fess932/homeLab/internal/importer"
	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/probe"
	"github.com/fess932/homeLab/internal/secrets"
	"github.com/fess932/homeLab/internal/store"
	"github.com/fess932/homeLab/internal/tsdb"
)

const setupToken = "setup-token-for-tests-0123456789abcdef"

type okProber struct{}

func (okProber) Probe(context.Context, model.Check) probe.Result { return probe.Result{OK: true} }

type harness struct {
	t      *testing.T
	srv    *httptest.Server
	client *http.Client
	csrf   string
	st     *store.Store
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	cfg := config.Config{Listen: ":8080", DataDir: dir, Retention: "30d", Version: "test"}
	st, err := store.Open(ctx, filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := secrets.NewBox(make([]byte, 32))
	log := slog.New(slog.DiscardHandler)
	// Supervisor не запускается: TSDB считается «стартующей», запросы метрик должны отвечать 503.
	sup := tsdb.NewSupervisor(tsdb.Options{}, log)
	client := tsdb.NewClient("127.0.0.1:1")
	sched := probe.NewScheduler(okProber{}, log)
	t.Cleanup(sched.Stop)
	assetSvc := &assets.Service{Dir: filepath.Join(dir, "assets"), Store: st}
	var mu sync.Mutex
	pending := true
	syncChecks := func() {
		checks, _ := st.ListChecks(context.Background())
		sched.Sync(checks)
	}
	srv := New(Deps{
		Config: cfg, Store: st, Box: box, Scheduler: sched, Supervisor: sup,
		Reconciler: &tsdb.Reconciler{Store: st, Box: box, Sup: sup, Log: log},
		Watcher:    tsdb.NewWatcher(client, st, sup, log),
		TSDB:       client, Assets: assetSvc,
		Importer: &importer.Service{Store: st, Assets: assetSvc, RevisionsDir: filepath.Join(dir, "rev"), OnApplied: syncChecks},
		Log:      log,
		SetupToken: func() (string, bool) {
			mu.Lock()
			defer mu.Unlock()
			return setupToken, pending
		},
		SetupDone: func() {
			mu.Lock()
			defer mu.Unlock()
			pending = false
		},
		OnChecks: syncChecks,
		Disk:     func() DiskUsage { return DiskUsage{Total: 100 << 30, Free: 50 << 30} },
	})
	hs := httptest.NewServer(srv)
	t.Cleanup(hs.Close)
	jar, _ := cookiejar.New(nil)
	return &harness{t: t, srv: hs, client: &http.Client{Jar: jar}, st: st}
}

type resp struct {
	status int
	header http.Header
	body   []byte
}

func (r resp) json(v any) {
	_ = json.Unmarshal(r.body, v)
}

func (r resp) code() string {
	var e struct {
		Code string `json:"code"`
	}
	r.json(&e)
	return e.Code
}

func (h *harness) req(method, path string, body any, hdr map[string]string) resp {
	h.t.Helper()
	var rd io.Reader
	ct := ""
	switch b := body.(type) {
	case nil:
	case *bytes.Buffer:
		rd = b
	default:
		raw, _ := json.Marshal(b)
		rd, ct = bytes.NewReader(raw), "application/json"
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, rd)
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if h.csrf != "" && method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", h.csrf)
	}
	for k, v := range hdr {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	r, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return resp{status: r.StatusCode, header: r.Header, body: b}
}

func (h *harness) expect(r resp, status int, code string) {
	h.t.Helper()
	if r.status != status || (code != "" && r.code() != code) {
		h.t.Fatalf("ожидалось %d %s, получено %d %s", status, code, r.status, r.body)
	}
}

func (h *harness) setup() {
	h.t.Helper()
	r := h.req("POST", "/api/v1/setup", map[string]string{"token": setupToken, "username": "admin", "password": "password1234", "title": "Дом"}, nil)
	h.expect(r, 200, "")
	var s struct {
		CSRF string `json:"csrf_token"`
	}
	r.json(&s)
	h.csrf = s.CSRF
}

func TestSetupAndSession(t *testing.T) {
	h := newHarness(t)
	var st struct{ Required bool }
	h.req("GET", "/api/v1/setup", nil, nil).json(&st)
	if !st.Required {
		t.Fatal("до setup required=true")
	}
	h.expect(h.req("GET", "/api/v1/pages", nil, nil), 401, "unauthorized")
	h.expect(h.req("POST", "/api/v1/setup", map[string]string{"token": "wrong", "username": "a", "password": "password1234"}, nil), 403, "forbidden")
	h.expect(h.req("POST", "/api/v1/setup", map[string]string{"token": setupToken, "username": "a", "password": "short"}, nil), 422, "validation")

	r := h.req("POST", "/api/v1/setup", map[string]string{"token": setupToken, "username": "admin", "password": "password1234", "title": "Дом"}, nil)
	h.expect(r, 200, "")
	cookie := r.header.Get("Set-Cookie")
	if !strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "SameSite=Lax") || strings.Contains(cookie, "Secure") {
		t.Fatalf("cookie по HTTP: %s", cookie)
	}
	var sess struct {
		CSRF string `json:"csrf_token"`
		User struct{ Username string }
	}
	r.json(&sess)
	if sess.CSRF == "" || sess.User.Username != "admin" {
		t.Fatalf("сессия: %s", r.body)
	}
	h.csrf = sess.CSRF

	// Setup-endpoint закрывается после создания администратора.
	h.expect(h.req("POST", "/api/v1/setup", map[string]string{"token": setupToken, "username": "b", "password": "password1234"}, nil), 409, "conflict")
	h.req("GET", "/api/v1/setup", nil, nil).json(&st)
	if st.Required {
		t.Fatal("после setup required=false")
	}
	h.expect(h.req("GET", "/api/v1/session", nil, nil), 200, "")
	var set model.Settings
	h.req("GET", "/api/v1/settings", nil, nil).json(&set)
	if set.Title != "Дом" || set.Runtime == nil || set.Runtime.Retention != "30d" {
		t.Fatalf("настройки: %+v", set)
	}

	h.expect(h.req("POST", "/api/v1/logout", nil, map[string]string{"X-CSRF-Token": ""}), 403, "csrf")
	h.expect(h.req("POST", "/api/v1/logout", nil, nil), 204, "")
	h.expect(h.req("GET", "/api/v1/session", nil, nil), 401, "unauthorized")
}

func TestLoginRateLimit(t *testing.T) {
	h := newHarness(t)
	h.setup()
	h.expect(h.req("POST", "/api/v1/login", map[string]string{"username": "admin", "password": "password1234"}, nil), 200, "")
	for range 4 {
		h.expect(h.req("POST", "/api/v1/login", map[string]string{"username": "admin", "password": "wrong-password"}, nil), 401, "unauthorized")
	}
	// Burst 5 исчерпан: следующая попытка отклоняется до проверки пароля, даже верного.
	h.expect(h.req("POST", "/api/v1/login", map[string]string{"username": "admin", "password": "password1234"}, nil), 429, "rate_limited")
}

func TestCSRFAndOrigin(t *testing.T) {
	h := newHarness(t)
	h.setup()
	page := map[string]any{"title": "Дом", "slug": "home", "theme": map[string]any{}, "groups": []any{}, "widgets": []any{}}
	h.expect(h.req("POST", "/api/v1/pages", page, map[string]string{"X-CSRF-Token": ""}), 403, "csrf")
	h.expect(h.req("POST", "/api/v1/pages", page, map[string]string{"X-CSRF-Token": "forged"}), 403, "csrf")
	// Запрос с чужого сайта отклоняется до проверки сессии, даже с верным токеном.
	h.expect(h.req("POST", "/api/v1/pages", page, map[string]string{"Sec-Fetch-Site": "cross-site"}), 403, "forbidden")
	h.expect(h.req("POST", "/api/v1/pages", page, map[string]string{"Origin": "http://evil.example"}), 403, "forbidden")
	h.expect(h.req("POST", "/api/v1/login", map[string]string{"username": "admin", "password": "x"}, map[string]string{"Sec-Fetch-Site": "cross-site"}), 403, "forbidden")
	h.expect(h.req("POST", "/api/v1/pages", page, map[string]string{"Sec-Fetch-Site": "same-origin"}), 201, "")
}

func TestPagesRevisions(t *testing.T) {
	h := newHarness(t)
	h.setup()
	in := map[string]any{"title": "Дом", "slug": "home", "theme": map[string]any{}, "groups": []any{map[string]any{"id": "new_1", "title": "G"}}, "widgets": []any{}}
	r := h.req("POST", "/api/v1/pages", in, nil)
	h.expect(r, 201, "")
	var p model.Page
	r.json(&p)
	if r.header.Get("ETag") != `"1"` {
		t.Fatalf("ETag: %q", r.header.Get("ETag"))
	}
	h.expect(h.req("PUT", "/api/v1/pages/"+p.ID, in, nil), 428, "precondition_required")
	h.expect(h.req("PUT", "/api/v1/pages/"+p.ID, in, map[string]string{"If-Match": `"1"`}), 200, "")
	// Ревизия 1 уже устарела: второе устройство получает конфликт, а не перезапись.
	h.expect(h.req("PUT", "/api/v1/pages/"+p.ID, in, map[string]string{"If-Match": `"1"`}), 409, "conflict")
	// Страница доступна и по slug.
	h.expect(h.req("GET", "/api/v1/pages/home", nil, nil), 200, "")

	bad := map[string]any{"title": "", "slug": "Bad Slug", "theme": map[string]any{}, "groups": []any{}, "widgets": []any{}}
	r = h.req("POST", "/api/v1/pages", bad, nil)
	h.expect(r, 422, "validation")
	var e struct{ Details map[string]string }
	r.json(&e)
	if e.Details["title"] == "" || e.Details["slug"] == "" {
		t.Fatalf("details: %s", r.body)
	}
	h.expect(h.req("GET", "/api/v1/pages/pg_missing", nil, nil), 404, "not_found")
	h.expect(h.req("GET", "/api/v1/nope", nil, nil), 404, "not_found")
}

func TestMetricsWhileTSDBStarting(t *testing.T) {
	h := newHarness(t)
	h.setup()
	r := h.req("POST", "/api/v1/metrics/query", map[string]any{"preset_id": "tpl_app_memory"}, nil)
	h.expect(r, 503, "tsdb_unavailable")
	h.expect(h.req("POST", "/api/v1/metrics/query", map[string]any{"preset_id": "tpl_nope"}, nil), 422, "validation")
	h.expect(h.req("POST", "/api/v1/metrics/query", map[string]any{"preset_id": "tpl_node_cpu"}, nil), 422, "validation")
	var st struct {
		TSDB struct{ State string }
		Disk struct{ Level string }
	}
	h.req("GET", "/api/v1/status", nil, nil).json(&st)
	if st.TSDB.State != tsdb.StateStarting || st.Disk.Level != "ok" {
		t.Fatalf("status: %+v", st)
	}
	h.expect(h.req("GET", "/readyz", nil, nil), 503, "")
	h.expect(h.req("GET", "/healthz", nil, nil), 200, "")
}

func (h *harness) createService(name string, sourceID *string) model.Service {
	h.t.Helper()
	r := h.req("POST", "/api/v1/services", map[string]any{"name": name, "url": "http://" + name + ".lan", "source_id": sourceID}, nil)
	h.expect(r, 201, "")
	var sv model.Service
	r.json(&sv)
	return sv
}

func (h *harness) createSource(name string, secretID *string) model.Source {
	h.t.Helper()
	r := h.req("POST", "/api/v1/sources", map[string]any{"name": name, "kind": "node_exporter", "url": "http://" + name + ":9100/metrics", "secret_id": secretID}, nil)
	h.expect(r, 201, "")
	var s model.Source
	r.json(&s)
	return s
}

func TestPublicPage(t *testing.T) {
	h := newHarness(t)
	h.setup()
	src := h.createSource("nas", nil)
	pub := h.createService("public", &src.ID)
	priv := h.createService("private", nil)
	in := map[string]any{"title": "Дом", "slug": "home", "theme": map[string]any{},
		"groups": []any{map[string]any{"id": "new_g1", "title": "A"}, map[string]any{"id": "new_g2", "title": "B"}},
		"widgets": []any{
			map[string]any{"id": "new_1", "group_id": "new_g1", "type": "link", "public": true, "config": map[string]any{"service_id": pub.ID}, "layout": map[string]any{}},
			map[string]any{"id": "new_2", "group_id": "new_g2", "type": "link", "public": false, "config": map[string]any{"service_id": priv.ID}, "layout": map[string]any{}},
		}}
	r := h.req("POST", "/api/v1/pages", in, nil)
	h.expect(r, 201, "")
	var p model.Page
	r.json(&p)

	anon := &harness{t: t, srv: h.srv, client: &http.Client{}}
	h.expect(anon.req("GET", "/api/v1/public", nil, nil), 404, "not_found")

	var set model.Settings
	r = h.req("GET", "/api/v1/settings", nil, nil)
	r.json(&set)
	set.PublicPageID = &p.ID
	h.expect(h.req("PUT", "/api/v1/settings", set, map[string]string{"If-Match": r.header.Get("ETag")}), 200, "")

	var view struct {
		Title    string
		Page     model.Page
		Services []model.Service
	}
	r = anon.req("GET", "/api/v1/public", nil, nil)
	h.expect(r, 200, "")
	r.json(&view)
	// Непубличные виджеты, пустые группы и несвязанные сервисы не раскрываются анониму.
	if len(view.Page.Widgets) != 1 || view.Page.Widgets[0].ID != p.Widgets[0].ID || len(view.Page.Groups) != 1 {
		t.Fatalf("публичная страница: %s", r.body)
	}
	if len(view.Services) != 1 || view.Services[0].ID != pub.ID || view.Services[0].SourceID != nil {
		t.Fatalf("сервисы: %s", r.body)
	}
	if strings.Contains(string(r.body), "private") {
		t.Fatal("приватный сервис в ответе")
	}
	h.expect(anon.req("GET", "/api/v1/public/widgets/"+p.Widgets[1].ID+"/data", nil, nil), 404, "not_found")
	h.expect(anon.req("GET", "/api/v1/public/widgets/"+p.Widgets[0].ID+"/data", nil, nil), 200, "")
	h.expect(anon.req("GET", "/api/v1/public/widgets/"+p.Widgets[0].ID+"/data?range=2y", nil, nil), 422, "validation")
	// Анонимный клиент не может выполнить произвольный запрос.
	h.expect(anon.req("POST", "/api/v1/metrics/query", map[string]any{"query": "up"}, nil), 401, "unauthorized")
	h.expect(anon.req("GET", "/api/v1/widgets/"+p.Widgets[1].ID+"/data", nil, nil), 401, "unauthorized")
}

func multipartBody(t *testing.T, fields map[string]string, files map[string][]byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	for k, v := range files {
		fw, _ := w.CreateFormFile(k, k+".bin")
		_, _ = fw.Write(v)
	}
	_ = w.Close()
	return &buf, w.FormDataContentType()
}

func pngBytes(t *testing.T) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (h *harness) upload(data []byte) resp {
	h.t.Helper()
	body, ct := multipartBody(h.t, nil, map[string][]byte{"file": data})
	return h.req("POST", "/api/v1/assets", body, map[string]string{"Content-Type": ct})
}

func TestAssets(t *testing.T) {
	h := newHarness(t)
	h.setup()
	r := h.upload(pngBytes(t))
	h.expect(r, 201, "")
	var a model.Asset
	r.json(&a)
	if a.MediaType != "image/png" || a.Width != 2 || a.Height != 3 || a.URL != "/assets/"+a.ID {
		t.Fatalf("asset: %+v", a)
	}
	// Повторная загрузка того же файла не создаёт дубль.
	var again model.Asset
	h.upload(pngBytes(t)).json(&again)
	if again.ID != a.ID {
		t.Fatal("дубликат по checksum")
	}
	anon := &harness{t: t, srv: h.srv, client: &http.Client{}}
	r = anon.req("GET", a.URL, nil, nil)
	if r.status != 200 || r.header.Get("Content-Type") != "image/png" || r.header.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(r.header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("выдача: %d %v", r.status, r.header)
	}

	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	h.expect(h.upload(svg), 415, "unsupported_media")
	h.expect(h.upload([]byte("просто текст")), 415, "unsupported_media")
	// PNG-заголовок с мусором после него: проверка по содержимому, а не по расширению.
	fake := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 100)...)
	h.expect(h.upload(fake), 415, "unsupported_media")

	big := append(pngBytes(t), bytes.Repeat([]byte{0}, assets.MaxSize)...)
	h.expect(h.upload(big), 413, "payload_too_large")
	huge := bytes.Repeat([]byte{0}, assets.MaxSize+1<<20)
	h.expect(h.upload(huge), 413, "payload_too_large")

	// Используемый файл не удаляется.
	h.createServiceWithIcon("x", "asset:"+a.ID)
	h.expect(h.req("DELETE", "/api/v1/assets/"+a.ID, nil, nil), 409, "in_use")
}

func (h *harness) createServiceWithIcon(name, icon string) {
	h.t.Helper()
	h.expect(h.req("POST", "/api/v1/services", map[string]any{"name": name, "url": "http://x.lan", "icon": icon}, nil), 201, "")
}

func (h *harness) preview(format string, file []byte, zip []byte) resp {
	h.t.Helper()
	files := map[string][]byte{"file": file}
	if zip != nil {
		files["assets"] = zip
	}
	body, ct := multipartBody(h.t, map[string]string{"format": format}, files)
	return h.req("POST", "/api/v1/import/preview", body, map[string]string{"Content-Type": ct})
}

func stripExportedAt(b []byte) string {
	var out []string
	for l := range strings.SplitSeq(string(b), "\n") {
		if !strings.HasPrefix(l, "exported_at:") {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

func TestImportRoundTrip(t *testing.T) {
	h := newHarness(t)
	h.setup()
	r := h.req("POST", "/api/v1/secrets", map[string]any{"name": "nas", "kind": "basic", "username": "scraper", "password": "S3cretValue!"}, nil)
	h.expect(r, 201, "")
	var sec model.Secret
	r.json(&sec)
	if strings.Contains(string(r.body), "S3cretValue") {
		t.Fatal("API вернул значение секрета")
	}
	src := h.createSource("nas", &sec.ID)
	sv := h.createService("nas", &src.ID)
	h.expect(h.req("POST", "/api/v1/checks", map[string]any{"service_id": sv.ID, "kind": "http", "target": "http://nas.lan/"}, nil), 201, "")
	h.expect(h.req("POST", "/api/v1/presets", map[string]any{"title": "Мой", "expression": `up{source_id="$source_id"}`, "unit": "bool"}, nil), 201, "")
	h.expect(h.req("POST", "/api/v1/pages", map[string]any{"title": "Дом", "slug": "home", "theme": map[string]any{"mode": "dark"},
		"groups":  []any{map[string]any{"id": "new_g", "title": "G"}},
		"widgets": []any{map[string]any{"id": "new_w", "group_id": "new_g", "type": "chart", "public": true, "config": map[string]any{"metric": map[string]any{"preset_id": "tpl_node_cpu", "vars": map[string]string{"source_id": src.ID}}, "range": "24h"}, "layout": map[string]any{"lg": map[string]int{"x": 0, "y": 0, "w": 6, "h": 2}}}},
	}, nil), 201, "")

	export := h.req("GET", "/api/v1/export", nil, nil)
	h.expect(export, 200, "")
	for _, leak := range []string{"S3cretValue", "password_hash", "argon2", "encrypted", "csrf", "token_hash"} {
		if strings.Contains(string(export.body), leak) {
			t.Fatalf("экспорт содержит %q", leak)
		}
	}
	if !strings.Contains(export.header.Get("Content-Disposition"), "attachment") {
		t.Fatal("экспорт должен скачиваться файлом")
	}

	r = h.preview("homedeck", export.body, nil)
	h.expect(r, 200, "")
	var pv importer.Preview
	r.json(&pv)
	if pv.Token == "" || len(pv.Changes) == 0 {
		t.Fatalf("preview: %s", r.body)
	}
	for _, c := range pv.Changes {
		if c.Action != "replace" {
			t.Fatalf("round-trip должен только заменять: %+v", c)
		}
	}
	r = h.req("POST", "/api/v1/import/apply", map[string]string{"token": pv.Token}, nil)
	h.expect(r, 200, "")
	var applied importer.Preview
	r.json(&applied)
	if !applied.Applied || applied.RevisionID == nil {
		t.Fatalf("apply: %s", r.body)
	}
	export2 := h.req("GET", "/api/v1/export", nil, nil)
	if stripExportedAt(export.body) != stripExportedAt(export2.body) {
		t.Fatalf("round-trip изменил настройки:\n%s\n---\n%s", export.body, export2.body)
	}
	h.expect(h.req("POST", "/api/v1/import/apply", map[string]string{"token": pv.Token}, nil), 409, "conflict")

	// Изменение базы после предпросмотра требует повторной проверки.
	r = h.preview("homedeck", export.body, nil)
	r.json(&pv)
	h.createService("late", nil)
	h.expect(h.req("POST", "/api/v1/import/apply", map[string]string{"token": pv.Token}, nil), 409, "conflict")

	var revs []model.ConfigRevision
	h.req("GET", "/api/v1/revisions", nil, nil).json(&revs)
	if len(revs) != 1 {
		t.Fatalf("ревизия перед заменой: %+v", revs)
	}

	bad := bytes.Replace(export.body, []byte("schema_version: 1"), []byte("schema_version: 2"), 1)
	h.expect(h.preview("homedeck", bad, nil), 422, "validation")
}

func TestHomerImport(t *testing.T) {
	h := newHarness(t)
	h.setup()
	homer := `
title: "Лаборатория"
subtitle: "Homer"
theme: default
stylesheet: ["assets/custom.css"]
services:
  - name: "Медиа"
    icon: "fas fa-film"
    items:
      - name: "Jellyfin"
        subtitle: "Кино"
        url: "http://jellyfin.lan"
        tag: "media"
        keywords: "tv movies"
        logo: "assets/tools/jellyfin.png"
        target: "_blank"
      - name: "Pi-hole"
        url: "http://pihole.lan/admin"
        type: "PiHole"
        target: "_self"
        tagstyle: "is-success"
        mystery: 1
      - name: "Сломанный"
        url: "not a url"
`
	var zbuf bytes.Buffer
	zw := newZip(&zbuf)
	zw("assets/tools/jellyfin.png", pngBytes(t))
	r := h.preview("homer", []byte(homer), zbuf.Bytes())
	h.expect(r, 200, "")
	var pv importer.Preview
	r.json(&pv)
	paths := map[string]bool{}
	for _, w := range pv.Warnings {
		paths[w.Path] = true
	}
	for _, want := range []string{"theme", "stylesheet", "subtitle", "services[0].icon", "services[0].items[1].type", "services[0].items[1].tagstyle", "services[0].items[1].mystery", "services[0].items[2]"} {
		if !paths[want] {
			t.Errorf("нет предупреждения для %s: %s", want, r.body)
		}
	}
	h.expect(h.req("POST", "/api/v1/import/apply", map[string]string{"token": pv.Token}, nil), 200, "")

	var services []model.Service
	h.req("GET", "/api/v1/services", nil, nil).json(&services)
	if len(services) != 2 {
		t.Fatalf("сервисы: %+v", services)
	}
	byName := map[string]model.Service{}
	for _, s := range services {
		byName[s.Name] = s
	}
	j := byName["Jellyfin"]
	if j.Description != "Кино" || j.OpenMode != "new_tab" || !strings.HasPrefix(j.Icon, "asset:") || fmt.Sprint(j.Tags) != "[media tv movies]" {
		t.Fatalf("Jellyfin: %+v", j)
	}
	if byName["Pi-hole"].OpenMode != "same_tab" {
		t.Fatal("target _self → same_tab")
	}
	var pages []model.PageSummary
	h.req("GET", "/api/v1/pages", nil, nil).json(&pages)
	if len(pages) != 1 || pages[0].Slug != "home" {
		t.Fatalf("страницы: %+v", pages)
	}
	var p model.Page
	h.req("GET", "/api/v1/pages/home", nil, nil).json(&p)
	if len(p.Groups) != 1 || p.Groups[0].Title != "Медиа" || len(p.Widgets) != 2 {
		t.Fatalf("страница: %+v", p)
	}
	var set model.Settings
	h.req("GET", "/api/v1/settings", nil, nil).json(&set)
	if set.Title != "Лаборатория" || set.StartPageID == nil || *set.StartPageID != p.ID {
		t.Fatalf("настройки: %+v", set)
	}
}

func TestSystemSourcesReadOnly(t *testing.T) {
	h := newHarness(t)
	h.setup()
	var list []model.Source
	h.req("GET", "/api/v1/sources", nil, nil).json(&list)
	if len(list) != 2 || !list[0].System || !list[1].System {
		t.Fatalf("системные источники: %+v", list)
	}
	body := map[string]any{"name": "x", "kind": "prometheus", "url": "http://x/metrics"}
	h.expect(h.req("PUT", "/api/v1/sources/tsdb", body, map[string]string{"If-Match": `"1"`}), 403, "forbidden")
	h.expect(h.req("DELETE", "/api/v1/sources/homedeck", nil, nil), 403, "forbidden")
	h.expect(h.req("DELETE", "/api/v1/presets/tpl_node_cpu", nil, nil), 403, "forbidden")
	// Проверка источника на loopback запрещена политикой исходящих соединений.
	r := h.req("POST", "/api/v1/sources/test", map[string]any{"name": "x", "kind": "prometheus", "url": "http://127.0.0.1:8428/metrics"}, nil)
	var res struct {
		OK        bool
		ErrorKind string `json:"error_kind"`
	}
	r.json(&res)
	if res.OK || res.ErrorKind != "forbidden_address" {
		t.Fatalf("loopback: %s", r.body)
	}
}

func TestSecretInUse(t *testing.T) {
	h := newHarness(t)
	h.setup()
	r := h.req("POST", "/api/v1/secrets", map[string]any{"name": "t", "kind": "bearer", "token": "abc"}, nil)
	var sec model.Secret
	r.json(&sec)
	if sec.Mask != "••••••" {
		t.Fatalf("маска: %+v", sec)
	}
	h.createSource("nas", &sec.ID)
	h.expect(h.req("DELETE", "/api/v1/secrets/"+sec.ID, nil, nil), 409, "in_use")
}

func TestCountSamples(t *testing.T) {
	n, bad := countSamples([]byte("# HELP a x\n# TYPE a gauge\na 1\nb{x=\"1\"} 2 1700000000\n\nc:d_e 3\n"))
	if n != 3 || bad != "" {
		t.Fatalf("%d %q", n, bad)
	}
	if _, bad := countSamples([]byte("<html>login</html>\n")); bad == "" {
		t.Fatal("HTML не формат Prometheus")
	}
	if DiskLevel(DiskUsage{Total: 100 << 30, Free: 15 << 30}) != "warning" || DiskLevel(DiskUsage{Total: 100 << 30, Free: 5 << 30}) != "critical" || DiskLevel(DiskUsage{Total: 4 << 30, Free: 900 << 20}) != "critical" {
		t.Fatal("уровни диска")
	}
}
