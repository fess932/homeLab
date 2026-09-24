package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/VictoriaMetrics/metrics"
	"github.com/fess932/homeLab/internal/assets"
	"github.com/fess932/homeLab/internal/auth"
	"github.com/fess932/homeLab/internal/config"
	"github.com/fess932/homeLab/internal/importer"
	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/probe"
	"github.com/fess932/homeLab/internal/secrets"
	"github.com/fess932/homeLab/internal/store"
	"github.com/fess932/homeLab/internal/tsdb"
)

const (
	sessionCookie = "homedeck_session"
	sessionTTL    = 30 * 24 * time.Hour
	maxJSONBody   = 1 << 20
)

type Deps struct {
	Config     config.Config
	Store      *store.Store
	Box        *secrets.Box
	Scheduler  *probe.Scheduler
	Supervisor *tsdb.Supervisor
	Reconciler *tsdb.Reconciler
	Watcher    *tsdb.Watcher
	TSDB       *tsdb.Client
	Assets     *assets.Service
	Importer   *importer.Service
	UI         fs.FS
	Log        *slog.Logger
	SetupToken func() (string, bool)
	SetupDone  func()
	OnChecks   func()
	Disk       func() DiskUsage
}

type Server struct {
	Deps
	loginLimiter *auth.Limiter
	setupLimiter *auth.Limiter
	perClient    sync.Map
	mux          *http.ServeMux
	tsdbVersion  func() string
}

type DiskUsage struct {
	Total, Free, Metrics, App int64
}

func New(d Deps) *Server {
	s := &Server{Deps: d, loginLimiter: auth.NewLimiter(10, 5), setupLimiter: auth.NewLimiter(5, 5), mux: http.NewServeMux()}
	var mu sync.Mutex
	var ver string
	s.tsdbVersion = func() string {
		mu.Lock()
		defer mu.Unlock()
		if ver == "" && d.Supervisor.Ready() {
			ver = d.TSDB.Version(context.Background())
		}
		return ver
	}
	s.routes()
	return s
}

type handler func(w http.ResponseWriter, r *http.Request) error

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ok\n") })
	m.HandleFunc("GET /readyz", s.readyz)

	m.Handle("GET /api/v1/setup", s.public(s.getSetup))
	m.Handle("POST /api/v1/setup", s.public(s.postSetup))
	m.Handle("POST /api/v1/login", s.public(s.login))
	m.Handle("POST /api/v1/logout", s.public(s.logout))
	m.Handle("GET /api/v1/session", s.authed(s.session))
	m.Handle("PUT /api/v1/password", s.authed(s.changePassword))

	m.Handle("GET /api/v1/settings", s.authed(s.getSettings))
	m.Handle("PUT /api/v1/settings", s.authed(s.putSettings))

	m.Handle("GET /api/v1/pages", s.authed(s.listPages))
	m.Handle("POST /api/v1/pages", s.authed(s.createPage))
	m.Handle("GET /api/v1/pages/{id}", s.authed(s.getPage))
	m.Handle("PUT /api/v1/pages/{id}", s.authed(s.updatePage))
	m.Handle("DELETE /api/v1/pages/{id}", s.authed(s.deletePage))
	m.Handle("GET /api/v1/widgets/{id}/data", s.authed(s.widgetData))

	m.Handle("GET /api/v1/services", s.authed(s.listServices))
	m.Handle("POST /api/v1/services", s.authed(s.createService))
	m.Handle("GET /api/v1/services/{id}", s.authed(s.getService))
	m.Handle("PUT /api/v1/services/{id}", s.authed(s.updateService))
	m.Handle("DELETE /api/v1/services/{id}", s.authed(s.deleteService))

	m.Handle("GET /api/v1/checks", s.authed(s.listChecks))
	m.Handle("POST /api/v1/checks", s.authed(s.createCheck))
	m.Handle("GET /api/v1/checks/{id}", s.authed(s.getCheck))
	m.Handle("PUT /api/v1/checks/{id}", s.authed(s.updateCheck))
	m.Handle("DELETE /api/v1/checks/{id}", s.authed(s.deleteCheck))

	m.Handle("GET /api/v1/sources", s.authed(s.listSources))
	m.Handle("POST /api/v1/sources", s.authed(s.createSource))
	m.Handle("POST /api/v1/sources/test", s.authed(s.testNewSource))
	m.Handle("GET /api/v1/sources/{id}", s.authed(s.getSource))
	m.Handle("PUT /api/v1/sources/{id}", s.authed(s.updateSource))
	m.Handle("DELETE /api/v1/sources/{id}", s.authed(s.deleteSource))
	m.Handle("POST /api/v1/sources/{id}/test", s.authed(s.testSource))

	m.Handle("GET /api/v1/secrets", s.authed(s.listSecrets))
	m.Handle("POST /api/v1/secrets", s.authed(s.createSecret))
	m.Handle("PUT /api/v1/secrets/{id}", s.authed(s.updateSecret))
	m.Handle("DELETE /api/v1/secrets/{id}", s.authed(s.deleteSecret))

	m.Handle("GET /api/v1/presets", s.authed(s.listPresets))
	m.Handle("POST /api/v1/presets", s.authed(s.createPreset))
	m.Handle("PUT /api/v1/presets/{id}", s.authed(s.updatePreset))
	m.Handle("DELETE /api/v1/presets/{id}", s.authed(s.deletePreset))

	m.Handle("GET /api/v1/status", s.authed(s.status))
	m.Handle("POST /api/v1/metrics/query", s.authed(s.query))
	m.Handle("POST /api/v1/metrics/query-range", s.authed(s.queryRange))

	m.Handle("GET /api/v1/assets", s.authed(s.listAssets))
	m.Handle("POST /api/v1/assets", s.authed(s.uploadAsset))
	m.Handle("DELETE /api/v1/assets/{id}", s.authed(s.deleteAsset))
	m.Handle("GET /assets/{id}", s.public(s.serveAsset))

	m.Handle("POST /api/v1/import/preview", s.authed(s.importPreview))
	m.Handle("POST /api/v1/import/apply", s.authed(s.importApply))
	m.Handle("GET /api/v1/export", s.authed(s.export))
	m.Handle("GET /api/v1/revisions", s.authed(s.listRevisions))

	m.Handle("GET /api/v1/public", s.public(s.publicPage))
	m.Handle("GET /api/v1/public/widgets/{id}/data", s.public(s.publicWidgetData))

	m.Handle("/api/", s.public(func(w http.ResponseWriter, r *http.Request) error {
		return &Error{Status: http.StatusNotFound, Code: "not_found", Message: "маршрут не найден"}
	}))
	m.Handle("/", s.spa())
}

var (
	requestsTotal = metrics.NewCounter(`homedeck_http_requests_total`)
	requestErrors = metrics.NewCounter(`homedeck_http_errors_total`)
)

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestsTotal.Inc()
	if len(s.Config.AllowedHosts) > 0 && !s.hostAllowed(r.Host) {
		http.Error(w, "host not allowed", http.StatusMisdirectedRequest)
		return
	}
	rid := auth.RandomToken()[:16]
	r = r.WithContext(context.WithValue(r.Context(), ctxRequestID{}, rid))
	h := w.Header()
	h.Set("X-Request-Id", rid)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	start := time.Now()
	sw := &statusWriter{ResponseWriter: w}
	s.mux.ServeHTTP(sw, r)
	if sw.status >= 500 {
		requestErrors.Inc()
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		s.Log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", sw.status, "duration_ms", time.Since(start).Milliseconds(), "request_id", rid)
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) hostAllowed(host string) bool {
	h := strings.ToLower(host)
	if hp, _, err := net.SplitHostPort(h); err == nil {
		h = hp
	}
	for _, a := range s.Config.AllowedHosts {
		if a == h || a == host {
			return true
		}
	}
	return false
}

type ctxRequestID struct{}
type ctxSession struct{}

type sessionInfo struct {
	store.Session
	tokenHash []byte
}

func requestID(r *http.Request) string {
	v, _ := r.Context().Value(ctxRequestID{}).(string)
	return v
}

func currentSession(r *http.Request) *sessionInfo {
	v, _ := r.Context().Value(ctxSession{}).(*sessionInfo)
	return v
}

type Error struct {
	Status  int
	Code    string
	Message string
	Details map[string]string
}

func (e *Error) Error() string { return e.Message }

func (s *Server) public(h handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isMutating(r.Method) && !s.sameOrigin(r) {
			s.writeError(w, r, &Error{Status: http.StatusForbidden, Code: "forbidden", Message: "запрос с другого сайта отклонён"})
			return
		}
		if sess := s.loadSession(r); sess != nil {
			r = r.WithContext(context.WithValue(r.Context(), ctxSession{}, sess))
		}
		if err := h(w, r); err != nil {
			s.writeError(w, r, err)
		}
	})
}

func (s *Server) authed(h handler) http.Handler {
	return s.public(func(w http.ResponseWriter, r *http.Request) error {
		sess := currentSession(r)
		if sess == nil {
			return &Error{Status: http.StatusUnauthorized, Code: "unauthorized", Message: "требуется вход"}
		}
		if isMutating(r.Method) && !auth.EqualTokens(r.Header.Get("X-CSRF-Token"), sess.CSRFToken) {
			return &Error{Status: http.StatusForbidden, Code: "csrf", Message: "отсутствует или неверен CSRF-токен, обновите страницу"}
		}
		return h(w, r)
	})
}

func isMutating(m string) bool {
	return m != http.MethodGet && m != http.MethodHead && m != http.MethodOptions
}

func (s *Server) sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin" || site == "none"
	}
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		return origin == ""
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := r.Host
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" && s.fromTrustedProxy(r) {
		host = fh
	}
	return strings.EqualFold(u.Host, host)
}

func (s *Server) loadSession(r *http.Request) *sessionInfo {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" || len(c.Value) > 100 {
		return nil
	}
	h := auth.HashToken(c.Value)
	sess, err := s.Store.SessionByHash(r.Context(), h)
	if err != nil {
		return nil
	}
	if time.Until(sess.ExpiresAt) < sessionTTL-24*time.Hour {
		_ = s.Store.ExtendSession(r.Context(), h, sessionTTL)
	}
	return &sessionInfo{Session: sess, tokenHash: h}
}

func (s *Server) clientIP(r *http.Request) string {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	addr := ap.Addr().Unmap()
	if !s.trusted(addr) {
		return addr.String()
	}
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for _, hop := range slices.Backward(hops) {
		a, err := netip.ParseAddr(strings.TrimSpace(hop))
		if err != nil {
			break
		}
		a = a.Unmap()
		if !s.trusted(a) {
			return a.String()
		}
		addr = a
	}
	return addr.String()
}

func (s *Server) trusted(a netip.Addr) bool {
	for _, p := range s.Config.TrustedProxies {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func (s *Server) fromTrustedProxy(r *http.Request) bool {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	return err == nil && s.trusted(ap.Addr().Unmap())
}

func (s *Server) isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return s.fromTrustedProxy(r) && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *Error
	var ve *model.ValidationError
	var qe *tsdb.QueryError
	switch {
	case errors.As(err, &ae):
	case errors.As(err, &ve):
		ae = &Error{Status: http.StatusUnprocessableEntity, Code: "validation", Message: "проверьте поля формы", Details: ve.Fields}
	case errors.As(err, &qe):
		status := map[string]int{
			tsdb.CodeBadQuery: http.StatusUnprocessableEntity, tsdb.CodeTooMany: http.StatusUnprocessableEntity,
			tsdb.CodeTimeout: http.StatusGatewayTimeout, tsdb.CodeUnavailable: http.StatusServiceUnavailable, tsdb.CodeTooLarge: http.StatusUnprocessableEntity,
		}[qe.Code]
		ae = &Error{Status: status, Code: qe.Code, Message: qe.Message}
	case errors.Is(err, model.ErrNotFound):
		ae = &Error{Status: http.StatusNotFound, Code: "not_found", Message: "объект не найден"}
	case errors.Is(err, model.ErrConflict):
		msg := "объект изменён в другом месте, обновите данные"
		if !strings.HasPrefix(err.Error(), model.ErrConflict.Error()) {
			msg = err.Error()
		} else if _, rest, ok := strings.Cut(err.Error(), ": "); ok {
			msg = rest
		}
		ae = &Error{Status: http.StatusConflict, Code: "conflict", Message: msg}
	case errors.Is(err, model.ErrInUse):
		ae = &Error{Status: http.StatusConflict, Code: "in_use", Message: "объект используется и не может быть удалён"}
	case errors.Is(err, model.ErrForbidden):
		ae = &Error{Status: http.StatusForbidden, Code: "forbidden", Message: "операция запрещена"}
	case errors.Is(err, importer.ErrTokenExpired):
		ae = &Error{Status: http.StatusConflict, Code: "conflict", Message: err.Error()}
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		ae = &Error{Status: http.StatusGatewayTimeout, Code: "timeout", Message: "превышено время ожидания"}
	default:
		s.Log.Error("request failed", "method", r.Method, "path", r.URL.Path, "request_id", requestID(r), "err", err)
		ae = &Error{Status: http.StatusInternalServerError, Code: "internal", Message: "внутренняя ошибка, подробности в журнале по request_id"}
	}
	body := map[string]any{"code": ae.Code, "message": ae.Message, "request_id": requestID(r)}
	if ae.Details != nil {
		body["details"] = ae.Details
	}
	writeJSON(w, ae.Status, body)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func decode(r *http.Request, v any) error {
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		return &Error{Status: http.StatusUnsupportedMediaType, Code: "unsupported_media", Message: "ожидается application/json"}
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, maxJSONBody))
	if err := dec.Decode(v); err != nil {
		return &Error{Status: http.StatusBadRequest, Code: "bad_request", Message: "некорректный JSON: " + err.Error()}
	}
	return nil
}

func ifMatch(r *http.Request) (int64, error) {
	v := strings.Trim(strings.TrimPrefix(r.Header.Get("If-Match"), "W/"), `"`)
	if v == "" {
		return 0, &Error{Status: http.StatusPreconditionRequired, Code: "precondition_required", Message: "нужен заголовок If-Match с ревизией"}
	}
	rev, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, &Error{Status: http.StatusBadRequest, Code: "bad_request", Message: "If-Match: ожидается номер ревизии"}
	}
	return rev, nil
}

func etag(w http.ResponseWriter, rev int64) {
	w.Header().Set("ETag", fmt.Sprintf(`"%d"`, rev))
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	res := map[string]string{"sqlite": "ok", "tsdb": "ok", "config": "ok"}
	ok := true
	if err := s.Store.Ping(ctx); err != nil {
		res["sqlite"], ok = err.Error(), false
	}
	if st := s.Supervisor.Status(); st.State != tsdb.StateRunning {
		res["tsdb"], ok = st.State, false
	}
	if cs, err := s.Store.ConfigState(ctx); err != nil {
		res["config"], ok = err.Error(), false
	} else if cs.Applied == 0 {
		res["config"], ok = "not applied", false
	}
	status := http.StatusOK
	if !ok {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, res)
}
