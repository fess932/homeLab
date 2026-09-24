package model

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	StateUnknown  = "unknown"
	StateUp       = "up"
	StateDown     = "down"
	StateStale    = "stale"
	StateDisabled = "disabled"
	StatePending  = "pending"
)

var (
	PresetVars  = []string{"source_id", "service_id", "device_id", "instance"}
	labelRe     = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	reserved    = []string{"source_id", "job", "instance", "service_id"}
	iconRe      = regexp.MustCompile(`^(builtin:[a-z0-9-]{1,40}|asset:ast_[a-z0-9]{14}|favicon)?$`)
	presetVarRe = regexp.MustCompile(`\$(source_id|service_id|device_id|instance)\b`)
)

type ServiceInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	URL         string   `json:"url"`
	Icon        string   `json:"icon"`
	Tags        []string `json:"tags"`
	OpenMode    string   `json:"open_mode"`
	SourceID    *string  `json:"source_id"`
}

type Service struct {
	ID string `json:"id"`
	ServiceInput
	Revision int64        `json:"revision"`
	CheckID  *string      `json:"check_id"`
	Status   *CheckStatus `json:"status"`
}

func (s *ServiceInput) Normalize() {
	s.Name = strings.TrimSpace(s.Name)
	s.Description = strings.TrimSpace(s.Description)
	s.URL = strings.TrimSpace(s.URL)
	if s.OpenMode == "" {
		s.OpenMode = "new_tab"
	}
	tags := make([]string, 0, len(s.Tags))
	seen := map[string]bool{}
	for _, t := range s.Tags {
		t = strings.TrimSpace(t)
		if t != "" && !seen[strings.ToLower(t)] {
			seen[strings.ToLower(t)] = true
			tags = append(tags, t)
		}
	}
	s.Tags = tags
	if s.SourceID != nil && *s.SourceID == "" {
		s.SourceID = nil
	}
}

func (s *ServiceInput) Validate() error {
	var v validator
	v.check(s.Name != "" && utf8.RuneCountInString(s.Name) <= 100, "name", "от 1 до 100 символов")
	v.check(utf8.RuneCountInString(s.Description) <= 500, "description", "до 500 символов")
	u, err := url.Parse(s.URL)
	v.check(err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "", "url", "адрес http:// или https://")
	v.check(iconRe.MatchString(s.Icon), "icon", "builtin:<имя>, asset:<id> или favicon")
	v.check(oneOf(s.OpenMode, "same_tab", "new_tab"), "open_mode", "same_tab или new_tab")
	v.check(len(s.Tags) <= 20, "tags", "не более 20 тегов")
	for i, t := range s.Tags {
		v.check(utf8.RuneCountInString(t) <= 40, fmt.Sprintf("tags[%d]", i), "до 40 символов")
	}
	return v.err()
}

type CheckInput struct {
	ServiceID      string `json:"service_id"`
	Kind           string `json:"kind"`
	Target         string `json:"target"`
	ExpectedStatus string `json:"expected_status"`
	IntervalS      int    `json:"interval_s"`
	TimeoutS       int    `json:"timeout_s"`
	Enabled        *bool  `json:"enabled"`
	CAPEM          string `json:"ca_pem"`
}

type Check struct {
	ID string `json:"id"`
	CheckInput
	Revision int64        `json:"revision"`
	Status   *CheckStatus `json:"status"`
}

type CheckStatus struct {
	State       string   `json:"state"`
	Pending     *string  `json:"pending"`
	Streak      int      `json:"streak"`
	LastRun     *Time    `json:"last_run"`
	LastSuccess *Time    `json:"last_success"`
	DurationMS  *float64 `json:"duration_ms"`
	HTTPStatus  *int     `json:"http_status"`
	Error       string   `json:"error"`
}

func (c *CheckInput) Normalize() {
	c.Target = strings.TrimSpace(c.Target)
	c.ExpectedStatus = strings.ReplaceAll(strings.TrimSpace(c.ExpectedStatus), " ", "")
	if c.ExpectedStatus == "" && c.Kind == "http" {
		c.ExpectedStatus = "200-399"
	}
	if c.Kind == "tcp" {
		c.ExpectedStatus = ""
	}
	if c.IntervalS == 0 {
		c.IntervalS = 30
	}
	if c.TimeoutS == 0 {
		c.TimeoutS = min(5, c.IntervalS-1)
	}
	if c.Enabled == nil {
		c.Enabled = new(true)
	}
}

func (c *CheckInput) Validate() error {
	var v validator
	v.check(c.ServiceID != "", "service_id", "обязательное поле")
	switch c.Kind {
	case "http":
		u, err := url.Parse(c.Target)
		v.check(err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil, "target", "адрес http:// или https:// без логина и пароля")
		_, err = ParseStatusRanges(c.ExpectedStatus)
		v.check(err == nil, "expected_status", "диапазоны кодов вида 200-399,401")
	case "tcp":
		v.check(validHostPort(c.Target), "target", "host:port")
	default:
		v.add("kind", "http или tcp")
	}
	v.check(c.IntervalS >= 5 && c.IntervalS <= 3600, "interval_s", "от 5 до 3600 секунд")
	v.check(c.TimeoutS >= 1 && c.TimeoutS < c.IntervalS, "timeout_s", "от 1 секунды и меньше интервала")
	if c.CAPEM != "" {
		v.check(validCA(c.CAPEM), "ca_pem", "PEM-сертификат CA")
	}
	return v.err()
}

type StatusRange struct{ From, To int }

func ParseStatusRanges(s string) ([]StatusRange, error) {
	var out []StatusRange
	for part := range strings.SplitSeq(s, ",") {
		from, to, isRange := strings.Cut(part, "-")
		if !isRange {
			to = from
		}
		a, err1 := strconv.Atoi(from)
		b, err2 := strconv.Atoi(to)
		if err1 != nil || err2 != nil || a < 100 || b > 599 || a > b {
			return nil, fmt.Errorf("некорректный диапазон %q", part)
		}
		out = append(out, StatusRange{a, b})
	}
	return out, nil
}

func StatusMatches(rs []StatusRange, code int) bool {
	for _, r := range rs {
		if code >= r.From && code <= r.To {
			return true
		}
	}
	return false
}

const (
	SourcePrometheus   = "prometheus"
	SourceNodeExporter = "node_exporter"
	SourceCAdvisor     = "cadvisor"
	SystemSourceApp    = "homedeck"
	SystemSourceTSDB   = "tsdb"
)

type SourceTLS struct {
	CAPEM      string `json:"ca_pem"`
	ServerName string `json:"server_name"`
}

type SourceInput struct {
	Name      string            `json:"name"`
	Kind      string            `json:"kind"`
	URL       string            `json:"url"`
	IntervalS int               `json:"interval_s"`
	TimeoutS  int               `json:"timeout_s"`
	Labels    map[string]string `json:"labels"`
	SecretID  *string           `json:"secret_id"`
	TLS       SourceTLS         `json:"tls"`
	Enabled   *bool             `json:"enabled"`
}

type SourceStatus struct {
	State       string   `json:"state"`
	LastAttempt *Time    `json:"last_attempt"`
	LastSuccess *Time    `json:"last_success"`
	DurationMS  *float64 `json:"duration_ms"`
	Samples     *int     `json:"samples"`
	Error       string   `json:"error"`
}

type Source struct {
	ID string `json:"id"`
	SourceInput
	System   bool         `json:"system"`
	Revision int64        `json:"revision"`
	Status   SourceStatus `json:"status"`
}

func (s *SourceInput) Normalize() {
	s.Name = strings.TrimSpace(s.Name)
	s.URL = strings.TrimSpace(s.URL)
	if s.IntervalS == 0 {
		s.IntervalS = 15
	}
	if s.TimeoutS == 0 {
		s.TimeoutS = min(5, s.IntervalS-1)
	}
	if s.Labels == nil {
		s.Labels = map[string]string{}
	}
	if s.SecretID != nil && *s.SecretID == "" {
		s.SecretID = nil
	}
	if s.Enabled == nil {
		s.Enabled = new(true)
	}
	s.TLS.ServerName = strings.TrimSpace(s.TLS.ServerName)
}

func (s *SourceInput) Validate() error {
	var v validator
	v.check(s.Name != "" && utf8.RuneCountInString(s.Name) <= 100, "name", "от 1 до 100 символов")
	v.check(oneOf(s.Kind, SourcePrometheus, SourceNodeExporter, SourceCAdvisor), "kind", "prometheus, node_exporter или cadvisor")
	u, err := url.Parse(s.URL)
	v.check(err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.Fragment == "",
		"url", "адрес http:// или https:// без логина и пароля; credentials задаются секретом")
	v.check(s.IntervalS >= 5 && s.IntervalS <= 3600, "interval_s", "от 5 до 3600 секунд")
	v.check(s.TimeoutS >= 1 && s.TimeoutS < s.IntervalS, "timeout_s", "от 1 секунды и меньше интервала")
	v.check(len(s.Labels) <= 20, "labels", "не более 20 меток")
	for k, val := range s.Labels {
		v.check(labelRe.MatchString(k) && !strings.HasPrefix(k, "__") && !oneOf(k, reserved...), "labels."+k, "недопустимое имя метки")
		v.check(utf8.RuneCountInString(val) <= 200, "labels."+k, "значение до 200 символов")
	}
	if s.TLS.CAPEM != "" {
		v.check(validCA(s.TLS.CAPEM), "tls.ca_pem", "PEM-сертификат CA")
	}
	return v.err()
}

// SecretKey — ключ шифрования устройства (например, local_key Tuya). В HTTP не отправляется.
const SecretKey = "key"

type SecretInput struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Username string `json:"username"`
	Password string `json:"password"`
	Token    string `json:"token"`
	Key      string `json:"key"`
}

type Secret struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Kind   string   `json:"kind"`
	Mask   string   `json:"mask"`
	UsedBy []string `json:"used_by"`
}

func (s *SecretInput) Validate() error {
	var v validator
	v.check(strings.TrimSpace(s.Name) != "" && utf8.RuneCountInString(s.Name) <= 100, "name", "от 1 до 100 символов")
	switch s.Kind {
	case "basic":
		v.check(s.Username != "" && len(s.Username) <= 256, "username", "обязательное поле")
		v.check(len(s.Password) <= 4096, "password", "слишком длинный")
	case "bearer":
		v.check(s.Token != "" && len(s.Token) <= 8192, "token", "обязательное поле")
	case SecretKey:
		v.check(s.Key != "" && len(s.Key) <= 256, "key", "обязательное поле")
	default:
		v.add("kind", "basic, bearer или key")
	}
	return v.err()
}

func (s SecretInput) Mask() string {
	if s.Kind == "basic" {
		return s.Username + " / ••••••"
	}
	return "••••••"
}

type Threshold struct {
	Value float64 `json:"value"`
	Color string  `json:"color"`
}

type PresetInput struct {
	Title      string      `json:"title"`
	Expression string      `json:"expression"`
	Unit       string      `json:"unit"`
	Legend     string      `json:"legend"`
	Thresholds []Threshold `json:"thresholds"`
	MinStepS   int         `json:"min_step_s"`
}

type Preset struct {
	ID string `json:"id"`
	PresetInput
	Builtin  bool     `json:"builtin"`
	Category string   `json:"category"`
	Vars     []string `json:"vars"`
	Revision int64    `json:"revision"`
}

var Units = []string{"", "percent", "percent_unit", "bytes", "bytes_per_second", "seconds", "milliseconds", "count", "per_second", "celsius", "bool", "ppm", "ugm3", "mgm3"}

func (p *PresetInput) Normalize() {
	p.Title = strings.TrimSpace(p.Title)
	p.Expression = strings.TrimSpace(p.Expression)
	if p.Thresholds == nil {
		p.Thresholds = []Threshold{}
	}
}

func (p *PresetInput) Validate() error {
	var v validator
	v.check(p.Title != "" && utf8.RuneCountInString(p.Title) <= 100, "title", "от 1 до 100 символов")
	v.check(p.Expression != "" && len(p.Expression) <= 4000, "expression", "от 1 до 4000 символов")
	v.check(oneOf(p.Unit, Units...), "unit", "неизвестная единица")
	v.check(utf8.RuneCountInString(p.Legend) <= 200, "legend", "до 200 символов")
	v.check(len(p.Thresholds) <= 5, "thresholds", "не более 5 порогов")
	for i, t := range p.Thresholds {
		v.check(oneOf(t.Color, "ok", "warn", "crit"), fmt.Sprintf("thresholds[%d].color", i), "ok, warn или crit")
	}
	v.check(p.MinStepS >= 0 && p.MinStepS <= 86400, "min_step_s", "от 0 до 86400")
	return v.err()
}

func PresetVarsOf(expr string) []string {
	var out []string
	for _, m := range presetVarRe.FindAllStringSubmatch(expr, -1) {
		if !oneOf(m[1], out...) {
			out = append(out, m[1])
		}
	}
	if out == nil {
		return []string{}
	}
	return out
}

func ExpandPreset(expr string, vars map[string]string) (string, error) {
	var missing string
	out := presetVarRe.ReplaceAllStringFunc(expr, func(m string) string {
		name := m[1:]
		val, ok := vars[name]
		if !ok || !varRe.MatchString(val) {
			missing = name
			return m
		}
		return val
	})
	if missing != "" {
		return "", Invalid("vars."+missing, "не задана переменная шаблона")
	}
	return out, nil
}

type Settings struct {
	Title           string       `json:"title"`
	LogoAssetID     *string      `json:"logo_asset_id"`
	StartPageID     *string      `json:"start_page_id"`
	PublicPageID    *string      `json:"public_page_id"`
	Revision        int64        `json:"revision"`
	RestartRequired []string     `json:"restart_required"`
	Runtime         *RuntimeInfo `json:"runtime,omitempty"`
}

type RuntimeInfo struct {
	Listen      string `json:"listen"`
	DataDir     string `json:"data_dir"`
	Retention   string `json:"retention"`
	Version     string `json:"version"`
	TSDBVersion string `json:"tsdb_version"`
}

func (s *Settings) Validate() error {
	var v validator
	s.Title = strings.TrimSpace(s.Title)
	v.check(s.Title != "" && utf8.RuneCountInString(s.Title) <= 100, "title", "от 1 до 100 символов")
	return v.err()
}

type Asset struct {
	ID        string `json:"id"`
	MediaType string `json:"media_type"`
	Size      int64  `json:"size"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Checksum  string `json:"checksum"`
	URL       string `json:"url"`
	CreatedAt Time   `json:"created_at"`
	Path      string `json:"-"`
}

type ConfigRevision struct {
	ID            int64  `json:"id"`
	SchemaVersion int    `json:"schema_version"`
	Reason        string `json:"reason"`
	CreatedAt     Time   `json:"created_at"`
}

func validHostPort(s string) bool {
	host, port, err := net.SplitHostPort(s)
	if err != nil || host == "" {
		return false
	}
	p, err := strconv.Atoi(port)
	return err == nil && p > 0 && p < 65536
}

func validCA(s string) bool {
	b, _ := pem.Decode([]byte(s))
	if b == nil || b.Type != "CERTIFICATE" {
		return false
	}
	_, err := x509.ParseCertificate(b.Bytes)
	return err == nil
}
