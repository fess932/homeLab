package model

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	WidgetLink   = "link"
	WidgetLinks  = "links"
	WidgetClock  = "clock"
	WidgetNote   = "note"
	WidgetNumber = "number"
	WidgetChart  = "chart"
	WidgetStatus = "status"
)

var (
	slugRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	accentRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	varRe    = regexp.MustCompile(`^[A-Za-z0-9_.:\-]{1,128}$`)
	ranges   = map[string]time.Duration{"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour}
)

func RangeDuration(name string) (time.Duration, bool) {
	d, ok := ranges[name]
	return d, ok
}

type Theme struct {
	Mode              string  `json:"mode"`
	Accent            string  `json:"accent"`
	BackgroundAssetID *string `json:"background_asset_id"`
	Density           string  `json:"density"`
	Columns           int     `json:"columns"`
}

func DefaultTheme() Theme {
	return Theme{Mode: "system", Accent: "#22c3e6", Density: "comfortable", Columns: 12}
}

type Rect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type Layout struct {
	LG *Rect `json:"lg,omitempty"`
	MD *Rect `json:"md,omitempty"`
	SM *Rect `json:"sm,omitempty"`
}

type Group struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Collapsed bool   `json:"collapsed"`
}

type Widget struct {
	ID      string          `json:"id"`
	GroupID string          `json:"group_id"`
	Type    string          `json:"type"`
	Config  json.RawMessage `json:"config"`
	Layout  Layout          `json:"layout"`
}

type PageInput struct {
	Title   string   `json:"title"`
	Slug    string   `json:"slug"`
	Order   int      `json:"order"`
	// Public — страница целиком открывается без входа по адресу /public/<slug>.
	Public  bool     `json:"public"`
	Theme   Theme    `json:"theme"`
	Groups  []Group  `json:"groups"`
	Widgets []Widget `json:"widgets"`
}

type Page struct {
	ID string `json:"id"`
	PageInput
	Revision  int64 `json:"revision"`
	UpdatedAt Time  `json:"updated_at"`
}

type PageSummary struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Slug     string `json:"slug"`
	Order    int    `json:"order"`
	Public   bool   `json:"public"`
	Revision int64  `json:"revision"`
}

type MetricRef struct {
	PresetID string            `json:"preset_id"`
	Vars     map[string]string `json:"vars"`
}

// LinkConfig — ссылка на сервис (с проверкой и статусом) или простая ссылка URL/Title без проверки.
type LinkConfig struct {
	ServiceID   string     `json:"service_id"`
	URL         string     `json:"url,omitempty"`
	Title       string     `json:"title,omitempty"`
	Icon        string     `json:"icon,omitempty"`
	ShowStatus  bool       `json:"show_status"`
	ShowLatency bool       `json:"show_latency"`
	Metric      *MetricRef `json:"metric"`
}

type LinksConfig struct {
	Title      string   `json:"title"`
	ServiceIDs []string `json:"service_ids"`
}

type ClockConfig struct {
	Timezone    string `json:"timezone"`
	Hour12      bool   `json:"hour12"`
	ShowDate    bool   `json:"show_date"`
	ShowSeconds bool   `json:"show_seconds"`
}

type NoteConfig struct {
	Markdown string `json:"markdown"`
}

type NumberConfig struct {
	Title    string    `json:"title"`
	Metric   MetricRef `json:"metric"`
	Decimals int       `json:"decimals"`
}

type ChartConfig struct {
	Title   string    `json:"title"`
	Metric  MetricRef `json:"metric"`
	Range   string    `json:"range"`
	Stacked bool      `json:"stacked"`
}

type StatusConfig struct {
	ServiceID string `json:"service_id"`
}

func (w Widget) MetricRef() *MetricRef {
	switch w.Type {
	case WidgetNumber:
		var c NumberConfig
		if json.Unmarshal(w.Config, &c) == nil {
			return &c.Metric
		}
	case WidgetChart:
		var c ChartConfig
		if json.Unmarshal(w.Config, &c) == nil {
			return &c.Metric
		}
	case WidgetLink:
		var c LinkConfig
		if json.Unmarshal(w.Config, &c) == nil {
			return c.Metric
		}
	}
	return nil
}

func (w Widget) ServiceIDs() []string {
	switch w.Type {
	case WidgetLink:
		var c LinkConfig
		if json.Unmarshal(w.Config, &c) == nil && c.ServiceID != "" {
			return []string{c.ServiceID}
		}
	case WidgetStatus:
		var c StatusConfig
		if json.Unmarshal(w.Config, &c) == nil && c.ServiceID != "" {
			return []string{c.ServiceID}
		}
	case WidgetLinks:
		var c LinksConfig
		if json.Unmarshal(w.Config, &c) == nil {
			return c.ServiceIDs
		}
	}
	return nil
}

func (w Widget) ChartRange() string {
	if w.Type == WidgetChart {
		var c ChartConfig
		if json.Unmarshal(w.Config, &c) == nil {
			return c.Range
		}
	}
	return ""
}

func (p *PageInput) Normalize() {
	p.Title = strings.TrimSpace(p.Title)
	p.Slug = strings.TrimSpace(p.Slug)
	d := DefaultTheme()
	if p.Theme.Mode == "" {
		p.Theme.Mode = d.Mode
	}
	if p.Theme.Accent == "" {
		p.Theme.Accent = d.Accent
	}
	if p.Theme.Density == "" {
		p.Theme.Density = d.Density
	}
	if p.Theme.Columns == 0 {
		p.Theme.Columns = d.Columns
	}
	if p.Groups == nil {
		p.Groups = []Group{}
	}
	if p.Widgets == nil {
		p.Widgets = []Widget{}
	}
	for i := range p.Groups {
		p.Groups[i].Title = strings.TrimSpace(p.Groups[i].Title)
	}
}

func (p *PageInput) Validate() error {
	var v validator
	v.check(p.Title != "" && utf8.RuneCountInString(p.Title) <= 100, "title", "от 1 до 100 символов")
	v.check(slugRe.MatchString(p.Slug), "slug", "латиница в нижнем регистре, цифры и дефис, до 40 символов")
	v.check(oneOf(p.Theme.Mode, "system", "light", "dark"), "theme.mode", "system, light или dark")
	v.check(accentRe.MatchString(p.Theme.Accent), "theme.accent", "цвет вида #RRGGBB")
	v.check(oneOf(p.Theme.Density, "comfortable", "compact"), "theme.density", "comfortable или compact")
	v.check(p.Theme.Columns == 4 || p.Theme.Columns == 6 || p.Theme.Columns == 8 || p.Theme.Columns == 12, "theme.columns", "4, 6, 8 или 12")
	v.check(len(p.Groups) <= 50, "groups", "не более 50 групп")
	v.check(len(p.Widgets) <= 300, "widgets", "не более 300 виджетов")

	groups := map[string]bool{}
	for i, g := range p.Groups {
		f := fmt.Sprintf("groups[%d]", i)
		v.check(g.ID != "" && len(g.ID) <= 64, f+".id", "обязательное поле")
		v.check(!groups[g.ID], f+".id", "повторяется")
		v.check(utf8.RuneCountInString(g.Title) <= 100, f+".title", "до 100 символов")
		groups[g.ID] = true
	}
	ids := map[string]bool{}
	for i, w := range p.Widgets {
		f := fmt.Sprintf("widgets[%d]", i)
		v.check(w.ID != "" && len(w.ID) <= 64, f+".id", "обязательное поле")
		v.check(!ids[w.ID], f+".id", "повторяется")
		ids[w.ID] = true
		v.check(groups[w.GroupID], f+".group_id", "группа не найдена")
		validateLayout(&v, f+".layout", w.Layout, p.Theme.Columns)
		validateWidgetConfig(&v, f+".config", w)
	}
	return v.err()
}

func validateLayout(v *validator, f string, l Layout, lgCols int) {
	for _, bp := range []struct {
		name string
		r    *Rect
		cols int
	}{{"lg", l.LG, lgCols}, {"md", l.MD, 6}, {"sm", l.SM, 1}} {
		if bp.r == nil {
			continue
		}
		r := bp.r
		v.check(r.X >= 0 && r.Y >= 0 && r.W >= 1 && r.H >= 1 && r.H <= 50 && r.Y <= 10000 && r.X+r.W <= bp.cols,
			f+"."+bp.name, fmt.Sprintf("позиция вне сетки из %d колонок", bp.cols))
	}
}

func validateWidgetConfig(v *validator, f string, w Widget) {
	if len(w.Config) == 0 || string(w.Config) == "null" {
		v.add(f, "обязательное поле")
		return
	}
	bad := func(err error) bool {
		if err != nil {
			v.add(f, "некорректный JSON: "+err.Error())
			return true
		}
		return false
	}
	switch w.Type {
	case WidgetLink:
		var c LinkConfig
		if bad(json.Unmarshal(w.Config, &c)) {
			return
		}
		if c.ServiceID == "" {
			u, err := url.Parse(c.URL)
			v.check(c.URL != "" && err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && len(c.URL) <= 2048,
				f+".url", "выберите сервис или укажите адрес http:// или https://")
		}
		v.check(utf8.RuneCountInString(c.Title) <= 100, f+".title", "до 100 символов")
		v.check(iconRe.MatchString(c.Icon), f+".icon", "builtin:<имя>, asset:<id> или favicon")
		if c.Metric != nil {
			validateMetricRef(v, f+".metric", *c.Metric)
		}
	case WidgetLinks:
		var c LinksConfig
		if bad(json.Unmarshal(w.Config, &c)) {
			return
		}
		v.check(len(c.ServiceIDs) <= 50, f+".service_ids", "не более 50 ссылок")
		v.check(utf8.RuneCountInString(c.Title) <= 100, f+".title", "до 100 символов")
	case WidgetClock:
		var c ClockConfig
		if bad(json.Unmarshal(w.Config, &c)) {
			return
		}
		if c.Timezone != "" {
			_, err := time.LoadLocation(c.Timezone)
			v.check(err == nil, f+".timezone", "неизвестный часовой пояс")
		}
	case WidgetNote:
		var c NoteConfig
		if bad(json.Unmarshal(w.Config, &c)) {
			return
		}
		v.check(utf8.RuneCountInString(c.Markdown) <= 10000, f+".markdown", "до 10000 символов")
	case WidgetNumber:
		var c NumberConfig
		if bad(json.Unmarshal(w.Config, &c)) {
			return
		}
		validateMetricRef(v, f+".metric", c.Metric)
		v.check(c.Decimals >= 0 && c.Decimals <= 6, f+".decimals", "от 0 до 6")
	case WidgetChart:
		var c ChartConfig
		if bad(json.Unmarshal(w.Config, &c)) {
			return
		}
		validateMetricRef(v, f+".metric", c.Metric)
		_, ok := ranges[c.Range]
		v.check(ok, f+".range", "1h, 6h, 24h, 7d или 30d")
	case WidgetStatus:
		var c StatusConfig
		if bad(json.Unmarshal(w.Config, &c)) {
			return
		}
		v.check(c.ServiceID != "", f+".service_id", "выберите сервис")
	default:
		v.add(f, "неизвестный тип виджета "+w.Type)
	}
}

func validateMetricRef(v *validator, f string, m MetricRef) {
	v.check(m.PresetID != "", f+".preset_id", "выберите шаблон")
	for k, val := range m.Vars {
		v.check(oneOf(k, PresetVars...), f+".vars."+k, "неизвестная переменная")
		v.check(varRe.MatchString(val), f+".vars."+k, "допустимы латиница, цифры и _.:-")
	}
}

func ValidateVars(vars map[string]string) error {
	var v validator
	validateMetricRef(&v, "vars", MetricRef{PresetID: "-", Vars: vars})
	return v.err()
}

func oneOf(s string, opts ...string) bool { return slices.Contains(opts, s) }
