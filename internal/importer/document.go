package importer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/fess932/homeLab/drivers"
	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/store"
	"go.yaml.in/yaml/v3"
)

// SchemaVersion 2 добавил устройства; документы версии 1 читаются как раньше.
const SchemaVersion = 2

var docIDRe = regexp.MustCompile(`^[a-z][a-z0-9]*_[a-z0-9_-]{1,50}$`)

type Document struct {
	SchemaVersion int          `json:"schema_version"`
	Kind          string       `json:"kind"`
	ExportedAt    string       `json:"exported_at,omitempty"`
	Settings      DocSettings  `json:"settings"`
	Pages         []DocPage    `json:"pages"`
	Services      []DocService `json:"services"`
	Sources       []DocSource  `json:"sources"`
	Devices       []DocDevice  `json:"devices"`
	Presets       []DocPreset  `json:"presets"`
}

type DocSettings struct {
	Title       string  `json:"title"`
	LogoAssetID *string `json:"logo_asset_id,omitempty"`
	StartPage   string  `json:"start_page,omitempty"`
	PublicPage  string  `json:"public_page,omitempty"`
}

type DocPage struct {
	ID string `json:"id"`
	model.PageInput
}

type DocCheck struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Target         string `json:"target"`
	ExpectedStatus string `json:"expected_status,omitempty"`
	IntervalS      int    `json:"interval_s"`
	TimeoutS       int    `json:"timeout_s"`
	Enabled        *bool  `json:"enabled"`
	CAPEM          string `json:"ca_pem,omitempty"`
}

type DocService struct {
	ID string `json:"id"`
	model.ServiceInput
	Check *DocCheck `json:"check,omitempty"`
}

type DocSource struct {
	ID string `json:"id"`
	model.SourceInput
}

type DocDevice struct {
	ID string `json:"id"`
	model.DeviceInput
}

type DocPreset struct {
	ID string `json:"id"`
	model.PresetInput
}

func Export(snap store.Snapshot, now time.Time) Document {
	slugs := map[string]string{}
	for _, p := range snap.Pages {
		slugs[p.ID] = p.Slug
	}
	doc := Document{
		SchemaVersion: SchemaVersion,
		Kind:          "homedeck",
		ExportedAt:    now.UTC().Format(time.RFC3339),
		Settings:      DocSettings{Title: snap.Settings.Title, LogoAssetID: snap.Settings.LogoAssetID},
		Pages:         []DocPage{},
		Services:      []DocService{},
		Sources:       []DocSource{},
		Devices:       []DocDevice{},
		Presets:       []DocPreset{},
	}
	if snap.Settings.StartPageID != nil {
		doc.Settings.StartPage = slugs[*snap.Settings.StartPageID]
	}
	if snap.Settings.PublicPageID != nil {
		doc.Settings.PublicPage = slugs[*snap.Settings.PublicPageID]
	}
	for _, p := range snap.Pages {
		doc.Pages = append(doc.Pages, DocPage{ID: p.ID, PageInput: p.PageInput})
	}
	checks := map[string]model.Check{}
	for _, c := range snap.Checks {
		checks[c.ServiceID] = c
	}
	for _, s := range snap.Services {
		ds := DocService{ID: s.ID, ServiceInput: s.ServiceInput}
		if c, ok := checks[s.ID]; ok {
			ds.Check = &DocCheck{ID: c.ID, Kind: c.Kind, Target: c.Target, ExpectedStatus: c.ExpectedStatus, IntervalS: c.IntervalS, TimeoutS: c.TimeoutS, Enabled: c.Enabled, CAPEM: c.CAPEM}
		}
		doc.Services = append(doc.Services, ds)
	}
	for _, s := range snap.Sources {
		doc.Sources = append(doc.Sources, DocSource{ID: s.ID, SourceInput: s.SourceInput})
	}
	for _, d := range snap.Devices {
		doc.Devices = append(doc.Devices, DocDevice{ID: d.ID, DeviceInput: d.DeviceInput})
	}
	for _, p := range snap.Presets {
		doc.Presets = append(doc.Presets, DocPreset{ID: p.ID, PresetInput: p.PresetInput})
	}
	return doc
}

func MarshalYAML(doc Document) ([]byte, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var generic yaml.Node
	if err := yaml.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	blockStyle(&generic)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&generic); err != nil {
		return nil, err
	}
	return append([]byte("# HomeDeck export: без секретов, хешей паролей и сессий\n"), buf.Bytes()...), enc.Close()
}

func blockStyle(n *yaml.Node) {
	n.Style = 0
	for _, c := range n.Content {
		blockStyle(c)
	}
}

func parseDocument(data []byte) (Document, error) {
	var generic any
	if err := yaml.Unmarshal(data, &generic); err != nil {
		return Document{}, fmt.Errorf("YAML: %w", err)
	}
	m, ok := generic.(map[string]any)
	if !ok {
		return Document{}, errors.New("ожидается YAML-документ HomeDeck")
	}
	ver, ok := m["schema_version"].(int)
	switch {
	case !ok:
		return Document{}, errors.New("schema_version обязателен")
	case ver > SchemaVersion:
		return Document{}, fmt.Errorf("schema_version %d новее поддерживаемой %d: обновите HomeDeck", ver, SchemaVersion)
	case ver < 1:
		return Document{}, fmt.Errorf("schema_version %d не поддерживается", ver)
	}
	raw, err := json.Marshal(generic)
	if err != nil {
		return Document{}, fmt.Errorf("документ содержит значения, не представимые в JSON: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var doc Document
	if err := dec.Decode(&doc); err != nil {
		return Document{}, fmt.Errorf("структура документа: %w", err)
	}
	if doc.Kind != "homedeck" {
		return Document{}, errors.New(`kind должен быть "homedeck"`)
	}
	return doc, nil
}

type Env struct {
	Current   store.Snapshot
	SecretIDs map[string]bool
	AssetIDs  map[string]bool
}

func buildHomeDeck(doc Document, env Env) (store.Snapshot, []Warning, error) {
	var warns []Warning
	fields := map[string]string{}
	fail := func(path string, err error) {
		if ve, ok := errors.AsType[*model.ValidationError](err); ok {
			for k, v := range ve.Fields {
				fields[path+"."+k] = v
			}
			return
		}
		fields[path] = err.Error()
	}
	snap := store.Snapshot{Settings: env.Current.Settings}
	snap.Settings.Title = strings.TrimSpace(doc.Settings.Title)
	if snap.Settings.Title == "" {
		snap.Settings.Title = env.Current.Settings.Title
	}
	snap.Settings.LogoAssetID = nil
	if id := doc.Settings.LogoAssetID; id != nil {
		if env.AssetIDs[*id] {
			snap.Settings.LogoAssetID = id
		} else {
			warns = append(warns, Warning{"settings.logo_asset_id", "логотип не найден среди загруженных файлов и сброшен"})
		}
	}

	ids := map[string]string{}
	claim := func(path, id, prefix string) bool {
		switch {
		case !docIDRe.MatchString(id) || !strings.HasPrefix(id, prefix+"_"):
			fields[path+".id"] = fmt.Sprintf("id вида %s_<латиница, цифры>", prefix)
			return false
		case ids[id] != "":
			fields[path+".id"] = "повторяется с " + ids[id]
			return false
		}
		ids[id] = path
		return true
	}

	sources := map[string]bool{}
	for i, s := range doc.Sources {
		path := fmt.Sprintf("sources[%d]", i)
		if !claim(path, s.ID, "src") {
			continue
		}
		in := s.SourceInput
		in.Normalize()
		if err := in.Validate(); err != nil {
			fail(path, err)
			continue
		}
		if in.SecretID != nil && !env.SecretIDs[*in.SecretID] {
			warns = append(warns, Warning{path + ".secret_id", "секрет не найден; источник импортирован без авторизации, задайте секрет заново"})
			in.SecretID = nil
		}
		sources[s.ID] = true
		snap.Sources = append(snap.Sources, model.Source{ID: s.ID, SourceInput: in})
	}

	for i, d := range doc.Devices {
		path := fmt.Sprintf("devices[%d]", i)
		if !claim(path, d.ID, "dev") {
			continue
		}
		in := d.DeviceInput
		in.Normalize()
		if in.SecretID != nil && !env.SecretIDs[*in.SecretID] {
			// Секреты в экспорт не попадают: без ключа устройство сохраняется выключенным.
			warns = append(warns, Warning{path + ".secret_id", "секрет не найден; устройство импортировано выключенным, задайте секрет заново"})
			in.SecretID = nil
			in.Enabled = new(false)
		}
		if err := in.Validate(); err != nil {
			fail(path, err)
			continue
		}
		if err := drivers.Validate(&in); err != nil {
			fail(path, err)
			continue
		}
		snap.Devices = append(snap.Devices, model.Device{ID: d.ID, DeviceInput: in})
	}

	services := map[string]bool{}
	for i, s := range doc.Services {
		path := fmt.Sprintf("services[%d]", i)
		if !claim(path, s.ID, "svc") {
			continue
		}
		in := s.ServiceInput
		in.Normalize()
		if err := in.Validate(); err != nil {
			fail(path, err)
			continue
		}
		if id, ok := strings.CutPrefix(in.Icon, "asset:"); ok && !env.AssetIDs[id] {
			warns = append(warns, Warning{path + ".icon", "иконка не найдена среди загруженных файлов и сброшена"})
			in.Icon = ""
		}
		if in.SourceID != nil && !sources[*in.SourceID] {
			warns = append(warns, Warning{path + ".source_id", "источник не найден в документе, связь удалена"})
			in.SourceID = nil
		}
		services[s.ID] = true
		snap.Services = append(snap.Services, model.Service{ID: s.ID, ServiceInput: in})
		if s.Check != nil {
			cpath := path + ".check"
			if !claim(cpath, s.Check.ID, "chk") {
				continue
			}
			ci := model.CheckInput{ServiceID: s.ID, Kind: s.Check.Kind, Target: s.Check.Target, ExpectedStatus: s.Check.ExpectedStatus,
				IntervalS: s.Check.IntervalS, TimeoutS: s.Check.TimeoutS, Enabled: s.Check.Enabled, CAPEM: s.Check.CAPEM}
			ci.Normalize()
			if err := ci.Validate(); err != nil {
				fail(cpath, err)
				continue
			}
			snap.Checks = append(snap.Checks, model.Check{ID: s.Check.ID, CheckInput: ci})
		}
	}

	presets := map[string]bool{}
	for i, p := range doc.Presets {
		path := fmt.Sprintf("presets[%d]", i)
		if !claim(path, p.ID, "qp") {
			continue
		}
		in := p.PresetInput
		in.Normalize()
		if err := in.Validate(); err != nil {
			fail(path, err)
			continue
		}
		presets[p.ID] = true
		snap.Presets = append(snap.Presets, model.Preset{ID: p.ID, PresetInput: in})
	}

	slugs := map[string]string{}
	for i, p := range doc.Pages {
		path := fmt.Sprintf("pages[%d]", i)
		if !claim(path, p.ID, "pg") {
			continue
		}
		in := p.PageInput
		in.Normalize()
		if err := in.Validate(); err != nil {
			fail(path, err)
			continue
		}
		if prev, dup := slugs[in.Slug]; dup {
			fields[path+".slug"] = "повторяется со страницей " + prev
			continue
		}
		slugs[in.Slug] = p.ID
		if id := in.Theme.BackgroundAssetID; id != nil && !env.AssetIDs[*id] {
			warns = append(warns, Warning{path + ".theme.background_asset_id", "фон не найден среди загруженных файлов и сброшен"})
			in.Theme.BackgroundAssetID = nil
		}
		for j, w := range in.Widgets {
			wpath := fmt.Sprintf("%s.widgets[%d]", path, j)
			for _, sid := range w.ServiceIDs() {
				if !services[sid] {
					warns = append(warns, Warning{wpath, "виджет ссылается на отсутствующий сервис " + sid})
				}
			}
			if m := w.MetricRef(); m != nil && !model.IsBuiltinPresetID(m.PresetID) && !presets[m.PresetID] {
				warns = append(warns, Warning{wpath, "виджет ссылается на отсутствующий запрос " + m.PresetID})
			}
		}
		snap.Pages = append(snap.Pages, model.Page{ID: p.ID, PageInput: in})
	}

	snap.Settings.StartPageID, snap.Settings.PublicPageID = nil, nil
	if id, ok := slugs[doc.Settings.StartPage]; ok {
		snap.Settings.StartPageID = &id
	} else if doc.Settings.StartPage != "" {
		warns = append(warns, Warning{"settings.start_page", "страница не найдена"})
	}
	if snap.Settings.StartPageID == nil && len(snap.Pages) > 0 {
		snap.Settings.StartPageID = &snap.Pages[0].ID
	}
	if id, ok := slugs[doc.Settings.PublicPage]; ok {
		snap.Settings.PublicPageID = &id
	} else if doc.Settings.PublicPage != "" {
		warns = append(warns, Warning{"settings.public_page", "страница не найдена"})
	}

	if len(fields) > 0 {
		return store.Snapshot{}, warns, &model.ValidationError{Fields: fields}
	}
	return snap, warns, nil
}

func diff[T any](entity string, cur, next []T, id func(T) string, name func(T) string) []Change {
	var out []Change
	old := map[string]T{}
	for _, c := range cur {
		old[id(c)] = c
	}
	seen := map[string]bool{}
	for _, n := range next {
		seen[id(n)] = true
		action := "create"
		if _, ok := old[id(n)]; ok {
			action = "replace"
		}
		out = append(out, Change{Entity: entity, Action: action, Name: name(n)})
	}
	for _, c := range cur {
		if !seen[id(c)] {
			out = append(out, Change{Entity: entity, Action: "delete", Name: name(c)})
		}
	}
	slices.SortStableFunc(out, func(a, b Change) int { return strings.Compare(actionOrder(a.Action), actionOrder(b.Action)) })
	return out
}

func actionOrder(a string) string {
	switch a {
	case "delete":
		return "0"
	case "replace":
		return "1"
	}
	return "2"
}
