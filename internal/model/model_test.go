package model

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

func page(widgets ...Widget) PageInput {
	p := PageInput{Title: "Дом", Slug: "home", Groups: []Group{{ID: "g1", Title: "Медиа"}}, Widgets: widgets}
	p.Normalize()
	return p
}

func widget(typ, cfg string, l Layout) Widget {
	return Widget{ID: "w1", GroupID: "g1", Type: typ, Config: json.RawMessage(cfg), Layout: l}
}

func fieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	ve, ok := errors.AsType[*ValidationError](err)
	if !ok {
		t.Fatalf("ожидалась ValidationError, получено %v", err)
	}
	return ve.Fields
}

func TestPageValidate(t *testing.T) {
	ok := page(widget(WidgetNote, `{"markdown":"hi"}`, Layout{LG: &Rect{0, 0, 12, 1}, MD: &Rect{0, 0, 6, 1}, SM: &Rect{0, 0, 1, 1}}))
	if err := ok.Validate(); err != nil {
		t.Fatalf("валидная страница: %v", err)
	}

	cases := []struct {
		name  string
		mod   func(*PageInput)
		field string
	}{
		{"пустой заголовок", func(p *PageInput) { p.Title = "" }, "title"},
		{"слаг с заглавной", func(p *PageInput) { p.Slug = "Home" }, "slug"},
		{"слаг с подчёркиванием", func(p *PageInput) { p.Slug = "my_home" }, "slug"},
		{"слаг начинается с дефиса", func(p *PageInput) { p.Slug = "-home" }, "slug"},
		{"колонки не из списка", func(p *PageInput) { p.Theme.Columns = 5 }, "theme.columns"},
		{"акцент не hex", func(p *PageInput) { p.Theme.Accent = "red" }, "theme.accent"},
		{"неизвестная группа", func(p *PageInput) { p.Widgets[0].GroupID = "nope" }, "widgets[0].group_id"},
		// md всегда 6 колонок, даже если lg шире.
		{"md вылезает за 6 колонок", func(p *PageInput) { p.Widgets[0].Layout.MD = &Rect{3, 0, 4, 1} }, "widgets[0].layout.md"},
		// sm — одна колонка: ширина 2 недопустима.
		{"sm шире одной колонки", func(p *PageInput) { p.Widgets[0].Layout.SM = &Rect{0, 0, 2, 1} }, "widgets[0].layout.sm"},
		{"lg учитывает theme.columns", func(p *PageInput) { p.Theme.Columns = 8 }, "widgets[0].layout.lg"},
		{"неизвестный тип", func(p *PageInput) { p.Widgets[0].Type = "iframe" }, "widgets[0].config"},
		{"пустой config", func(p *PageInput) { p.Widgets[0].Config = nil }, "widgets[0].config"},
		{"дубликат id виджета", func(p *PageInput) { p.Widgets = append(p.Widgets, p.Widgets[0]) }, "widgets[1].id"},
		{"дубликат id группы", func(p *PageInput) { p.Groups = append(p.Groups, p.Groups[0]) }, "groups[1].id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := ok
			p.Widgets = slices.Clone(ok.Widgets)
			p.Groups = slices.Clone(ok.Groups)
			c.mod(&p)
			f := fieldsOf(t, p.Validate())
			if _, has := f[c.field]; !has {
				t.Fatalf("нет ошибки поля %s: %v", c.field, f)
			}
		})
	}
}

func TestWidgetConfigs(t *testing.T) {
	cases := []struct {
		typ, cfg string
		field    string
	}{
		{WidgetLink, `{"service_id":"svc_1"}`, ""},
		{WidgetLink, `{}`, "url"},
		{WidgetLink, `{"url":"http://router.lan","title":"Роутер"}`, ""},
		{WidgetLink, `{"url":"javascript:alert(1)"}`, "url"},
		{WidgetLink, `{"url":"router.lan"}`, "url"},
		{WidgetClock, `{"timezone":"Europe/Moscow"}`, ""},
		{WidgetClock, `{"timezone":"Mars/Olympus"}`, "timezone"},
		{WidgetNote, `{"markdown":"` + strings.Repeat("a", 10001) + `"}`, "markdown"},
		{WidgetChart, `{"metric":{"preset_id":"tpl_node_cpu","vars":{"source_id":"src_1"}},"range":"24h"}`, ""},
		{WidgetChart, `{"metric":{"preset_id":"tpl_node_cpu"},"range":"2h"}`, "range"},
		{WidgetNumber, `{"metric":{"preset_id":""},"decimals":1}`, "metric.preset_id"},
		{WidgetNumber, `{"metric":{"preset_id":"x"},"decimals":9}`, "decimals"},
		// Значение переменной подставляется в MetricsQL внутри кавычек — кавычка должна отклоняться.
		{WidgetNumber, `{"metric":{"preset_id":"x","vars":{"source_id":"a\"} or vector(1) #"}}}`, "metric.vars.source_id"},
		{WidgetNumber, `{"metric":{"preset_id":"x","vars":{"job":"a"}}}`, "metric.vars.job"},
		{WidgetStatus, `{"service_id":""}`, "service_id"},
		{WidgetLinks, `{"service_ids":["a","b"]}`, ""},
		{WidgetLink, `not json`, ""},
	}
	for _, c := range cases {
		t.Run(c.typ+" "+c.cfg[:min(len(c.cfg), 40)], func(t *testing.T) {
			p := page(widget(c.typ, c.cfg, Layout{}))
			err := p.Validate()
			if c.cfg == "not json" {
				if err == nil {
					t.Fatal("некорректный JSON должен отклоняться")
				}
				return
			}
			if c.field == "" {
				if err != nil {
					t.Fatalf("ожидался успех: %v", err)
				}
				return
			}
			f := fieldsOf(t, err)
			if _, has := f["widgets[0].config."+c.field]; !has {
				t.Fatalf("нет ошибки %s: %v", c.field, f)
			}
		})
	}
}

func TestCheckValidate(t *testing.T) {
	base := func() CheckInput {
		c := CheckInput{ServiceID: "svc_1", Kind: "http", Target: "https://nas.lan/"}
		c.Normalize()
		return c
	}
	c := base()
	if c.ExpectedStatus != "200-399" || c.IntervalS != 30 || c.TimeoutS != 5 || !*c.Enabled {
		t.Fatalf("значения по умолчанию: %+v", c)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		mod   func(*CheckInput)
		field string
	}{
		{"timeout равен интервалу", func(c *CheckInput) { c.TimeoutS = 30 }, "timeout_s"},
		{"интервал меньше 5", func(c *CheckInput) { c.IntervalS = 4 }, "interval_s"},
		{"интервал больше часа", func(c *CheckInput) { c.IntervalS = 3601 }, "interval_s"},
		{"логин в URL", func(c *CheckInput) { c.Target = "http://u:p@nas/" }, "target"},
		{"не http", func(c *CheckInput) { c.Target = "ftp://nas/" }, "target"},
		{"кривой диапазон", func(c *CheckInput) { c.ExpectedStatus = "399-200" }, "expected_status"},
		{"tcp без порта", func(c *CheckInput) { c.Kind, c.Target = "tcp", "nas" }, "target"},
		{"tcp порт 0", func(c *CheckInput) { c.Kind, c.Target = "tcp", "nas:0" }, "target"},
		{"неизвестный kind", func(c *CheckInput) { c.Kind = "icmp" }, "kind"},
		{"мусорный CA", func(c *CheckInput) { c.CAPEM = "-----BEGIN CERTIFICATE-----\nAAA\n-----END CERTIFICATE-----" }, "ca_pem"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base()
			tc.mod(&c)
			if _, has := fieldsOf(t, c.Validate())[tc.field]; !has {
				t.Fatalf("нет ошибки %s", tc.field)
			}
		})
	}
	tcp := CheckInput{ServiceID: "svc_1", Kind: "tcp", Target: "nas:22", ExpectedStatus: "200"}
	tcp.Normalize()
	if tcp.ExpectedStatus != "" {
		t.Error("у TCP ожидаемый код не хранится")
	}
	if err := tcp.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSourceValidate(t *testing.T) {
	base := func() SourceInput {
		s := SourceInput{Name: "nas", Kind: SourceNodeExporter, URL: "http://nas:9100/metrics"}
		s.Normalize()
		return s
	}
	s := base()
	if s.IntervalS != 15 || s.TimeoutS != 5 || !*s.Enabled || s.Labels == nil {
		t.Fatalf("значения по умолчанию: %+v", s)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	// Служебные метки назначаются сервером; пользователь не должен их переопределять.
	for _, l := range []string{"source_id", "job", "instance", "service_id", "__address__", "1bad", "a-b"} {
		t.Run("label "+l, func(t *testing.T) {
			s := base()
			s.Labels = map[string]string{l: "x"}
			if _, has := fieldsOf(t, s.Validate())["labels."+l]; !has {
				t.Fatalf("метка %s должна отклоняться", l)
			}
		})
	}
	cases := []struct {
		name  string
		mod   func(*SourceInput)
		field string
	}{
		{"userinfo", func(s *SourceInput) { s.URL = "http://admin:pw@nas:9100/metrics" }, "url"},
		{"fragment", func(s *SourceInput) { s.URL = "http://nas:9100/metrics#x" }, "url"},
		{"timeout >= interval", func(s *SourceInput) { s.IntervalS, s.TimeoutS = 10, 10 }, "timeout_s"},
		{"kind", func(s *SourceInput) { s.Kind = "snmp" }, "kind"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := base()
			tc.mod(&s)
			if _, has := fieldsOf(t, s.Validate())[tc.field]; !has {
				t.Fatalf("нет ошибки %s", tc.field)
			}
		})
	}
}

func TestParseStatusRanges(t *testing.T) {
	rs, err := ParseStatusRanges("200-299,401,418")
	if err != nil {
		t.Fatal(err)
	}
	for code, want := range map[int]bool{200: true, 299: true, 300: false, 401: true, 418: true, 500: false} {
		if StatusMatches(rs, code) != want {
			t.Errorf("%d: ожидалось %v", code, want)
		}
	}
	for _, bad := range []string{"", "abc", "99", "600", "300-200", "200-", ",200"} {
		if _, err := ParseStatusRanges(bad); err == nil {
			t.Errorf("%q должен отклоняться", bad)
		}
	}
}

func TestExpandPreset(t *testing.T) {
	expr := `up{source_id="$source_id"} and on() $source_id_extra`
	out, err := ExpandPreset(`rate(x{source_id="$source_id",service_id="$service_id"})`, map[string]string{"source_id": "src_a", "service_id": "svc_b"})
	if err != nil || out != `rate(x{source_id="src_a",service_id="svc_b"})` {
		t.Fatalf("подстановка: %q %v", out, err)
	}
	// $source_id_extra — не переменная: граница слова не должна давать частичную замену.
	out, err = ExpandPreset(expr, map[string]string{"source_id": "s"})
	if err != nil || !strings.Contains(out, "$source_id_extra") || !strings.Contains(out, `source_id="s"`) {
		t.Fatalf("граница слова: %q %v", out, err)
	}
	if _, err := ExpandPreset(`up{source_id="$source_id"}`, nil); err == nil {
		t.Fatal("отсутствующая переменная должна давать ошибку")
	}
	// Инъекция через значение переменной.
	if _, err := ExpandPreset(`up{source_id="$source_id"}`, map[string]string{"source_id": `x"} or vector(1) or up{a="`}); err == nil {
		t.Fatal("кавычка в значении должна отклоняться")
	}
	if got := PresetVarsOf(`a{x="$instance"} + b{y="$instance",z="$service_id"}`); !slices.Equal(got, []string{"instance", "service_id"}) {
		t.Fatalf("PresetVarsOf: %v", got)
	}
	if got := PresetVarsOf(`up`); got == nil || len(got) != 0 {
		t.Fatalf("без переменных должен быть пустой непустой-nil срез: %#v", got)
	}
}

func TestLegendName(t *testing.T) {
	labels := map[string]string{"__name__": "m", "job": "j", "instance": "h:9100", "source_id": "s", "device": "eth0", "room": "hall"}
	cases := []struct {
		tpl  string
		lbl  map[string]string
		want string
	}{
		{"{{instance}} {{device}}", labels, "h:9100 eth0"},
		{"{{ device }}/{{missing}}", labels, "eth0/"},
		{"{{broken", labels, "{{broken"},
		{"", labels, "device=eth0, room=hall"},
		{"", map[string]string{"__name__": "m", "instance": "h"}, "h"},
		{"", map[string]string{"__name__": "m"}, "m"},
		{"", map[string]string{}, "значение"},
	}
	for _, c := range cases {
		if got := LegendName(c.tpl, c.lbl); got != c.want {
			t.Errorf("LegendName(%q) = %q, ожидалось %q", c.tpl, got, c.want)
		}
	}
}

func TestBuiltinPresets(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range BuiltinPresets() {
		if seen[p.ID] {
			t.Errorf("повтор id %s", p.ID)
		}
		seen[p.ID] = true
		if !IsBuiltinPresetID(p.ID) || !p.Builtin {
			t.Errorf("%s: встроенный шаблон должен иметь префикс tpl_", p.ID)
		}
		in := p.PresetInput
		if err := in.Validate(); err != nil {
			t.Errorf("%s: %v", p.ID, err)
		}
		if !slices.Equal(p.Vars, PresetVarsOf(p.Expression)) {
			t.Errorf("%s: vars %v не совпадают с выражением", p.ID, p.Vars)
		}
		vars := map[string]string{}
		for _, v := range p.Vars {
			vars[v] = "x_1"
		}
		if _, err := ExpandPreset(p.Expression, vars); err != nil {
			t.Errorf("%s: %v", p.ID, err)
		}
		if got, ok := BuiltinPreset(p.ID); !ok || got.Expression != p.Expression {
			t.Errorf("%s: BuiltinPreset не находит шаблон", p.ID)
		}
	}
	// Экраны мониторинга UI показывают первые три процентных шаблона узла: CPU, RAM, ФС.
	var pct []string
	for _, p := range BuiltinPresets() {
		if p.Category == "node" && p.Unit == "percent" {
			pct = append(pct, p.ID)
		}
	}
	if len(pct) < 3 || pct[0] != "tpl_node_cpu" || pct[1] != "tpl_node_memory" || pct[2] != "tpl_node_fs" {
		t.Errorf("порядок процентных шаблонов узла: %v", pct)
	}
}

func TestSecretValidate(t *testing.T) {
	ok := []SecretInput{{Name: "a", Kind: "basic", Username: "u"}, {Name: "b", Kind: "bearer", Token: "t"}}
	for _, s := range ok {
		if err := s.Validate(); err != nil {
			t.Errorf("%+v: %v", s, err)
		}
	}
	bad := []SecretInput{{Name: "", Kind: "basic", Username: "u"}, {Name: "a", Kind: "basic"}, {Name: "a", Kind: "bearer"}, {Name: "a", Kind: "oauth"}}
	for _, s := range bad {
		if err := s.Validate(); err == nil {
			t.Errorf("%+v должен отклоняться", s)
		}
	}
	if m := (SecretInput{Kind: "basic", Username: "admin", Password: "secret"}).Mask(); strings.Contains(m, "secret") || !strings.Contains(m, "admin") {
		t.Errorf("маска раскрывает пароль: %q", m)
	}
}

func TestNewID(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := NewID("svc")
		if len(id) != 18 || !strings.HasPrefix(id, "svc_") || seen[id] {
			t.Fatalf("плохой id %q", id)
		}
		seen[id] = true
	}
}
