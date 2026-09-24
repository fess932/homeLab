package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/store"
	"go.yaml.in/yaml/v3"
)

type homerConfig struct {
	Title    string       `yaml:"title"`
	Subtitle string       `yaml:"subtitle"`
	Logo     string       `yaml:"logo"`
	Services []homerGroup `yaml:"services"`
}

type homerGroup struct {
	Name  string      `yaml:"name"`
	Items []homerItem `yaml:"items"`
}

type homerItem struct {
	Name     string `yaml:"name"`
	Subtitle string `yaml:"subtitle"`
	URL      string `yaml:"url"`
	Target   string `yaml:"target"`
	Tag      string `yaml:"tag"`
	Keywords string `yaml:"keywords"`
	Logo     string `yaml:"logo"`
	Type     string `yaml:"type"`
}

var (
	homerTop       = map[string]bool{"title": true, "subtitle": true, "logo": true, "services": true, "documentTitle": true}
	homerGroupKeys = map[string]bool{"name": true, "items": true}
	homerItemKeys  = map[string]bool{"name": true, "subtitle": true, "url": true, "target": true, "tag": true, "keywords": true, "logo": true, "type": true}
	homerIgnored   = map[string]string{
		"header": "оформление Homer не переносится", "footer": "оформление Homer не переносится", "columns": "число колонок задаётся темой страницы",
		"connectivityCheck": "проверки доступности настраиваются отдельно", "theme": "тема Homer не переносится", "colors": "цвета Homer не переносятся",
		"stylesheet": "custom CSS не поддерживается", "defaults": "настройки Homer по умолчанию не переносятся", "message": "сообщение Homer не переносится",
		"links": "ссылки навигации Homer не переносятся", "icon": "иконки Font Awesome не поддерживаются", "tagstyle": "стиль тега не переносится",
		"class": "CSS-классы не поддерживаются", "background": "фон карточки не переносится", "externalConfig": "внешние конфигурации Homer не поддерживаются",
		"proxy": "настройки proxy Homer не переносятся", "hotkey": "горячие клавиши Homer не переносятся",
	}
)

type assetSaver interface {
	SaveBytes(ctx context.Context, data []byte) (model.Asset, error)
}

type homerAssets struct {
	files map[string]*zip.File
}

func openHomerAssets(zipData []byte) (*homerAssets, error) {
	if len(zipData) == 0 {
		return &homerAssets{files: map[string]*zip.File{}}, nil
	}
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("архив иконок: %w", err)
	}
	a := &homerAssets{files: map[string]*zip.File{}}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		a.files[path.Clean(strings.TrimPrefix(f.Name, "/"))] = f
	}
	return a, nil
}

func (a *homerAssets) find(ref string) *zip.File {
	ref = path.Clean(strings.TrimPrefix(strings.TrimPrefix(ref, "./"), "/"))
	if f, ok := a.files[ref]; ok {
		return f
	}
	for name, f := range a.files {
		if strings.HasSuffix(name, "/"+ref) || strings.HasSuffix(ref, "/"+name) {
			return f
		}
	}
	return nil
}

func (a *homerAssets) read(f *zip.File) ([]byte, error) {
	if f.UncompressedSize64 > 5<<20 {
		return nil, errors.New("файл больше 5 MiB")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, 5<<20+1))
}

type homerPlan struct {
	snap  store.Snapshot
	icons map[string]*zip.File
	logo  *zip.File
	zip   []byte
}

func parseHomer(data, zipData []byte, cur store.Snapshot) (homerPlan, []Warning, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return homerPlan{}, nil, fmt.Errorf("YAML: %w", err)
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return homerPlan{}, nil, errors.New("ожидается конфигурация Homer (config.yml)")
	}
	var warns []Warning
	reportUnknown(root.Content[0], "", homerTop, &warns)
	if svc := mapValue(root.Content[0], "services"); svc != nil && svc.Kind == yaml.SequenceNode {
		for gi, g := range svc.Content {
			gp := fmt.Sprintf("services[%d]", gi)
			reportUnknown(g, gp, homerGroupKeys, &warns)
			if items := mapValue(g, "items"); items != nil && items.Kind == yaml.SequenceNode {
				for ii, it := range items.Content {
					reportUnknown(it, fmt.Sprintf("%s.items[%d]", gp, ii), homerItemKeys, &warns)
				}
			}
		}
	}
	var cfg homerConfig
	if err := root.Decode(&cfg); err != nil {
		return homerPlan{}, warns, fmt.Errorf("структура Homer: %w", err)
	}
	assets, err := openHomerAssets(zipData)
	if err != nil {
		return homerPlan{}, warns, err
	}

	plan := homerPlan{icons: map[string]*zip.File{}, zip: zipData}
	snap := store.Snapshot{Settings: cur.Settings, Sources: cur.Sources, Presets: cur.Presets}
	if t := strings.TrimSpace(cfg.Title); t != "" {
		snap.Settings.Title = t
	}
	if cfg.Subtitle != "" {
		warns = append(warns, Warning{"subtitle", "подзаголовок не переносится"})
	}
	if cfg.Logo != "" {
		if f := assets.find(cfg.Logo); f != nil {
			plan.logo = f
		} else {
			warns = append(warns, Warning{"logo", "файл " + cfg.Logo + " не найден в архиве иконок"})
		}
	}
	page := model.Page{ID: model.NewID("pg"), Title: "Главная", Slug: "home", Theme: model.DefaultTheme()}
	for gi, g := range cfg.Services {
		gid := fmt.Sprintf("new_g%d", gi)
		title := strings.TrimSpace(g.Name)
		page.Groups = append(page.Groups, model.Group{ID: gid, Title: title})
		for ii, it := range g.Items {
			ipath := fmt.Sprintf("services[%d].items[%d]", gi, ii)
			svc := model.ServiceInput{
				Name:        strings.TrimSpace(it.Name),
				Description: strings.TrimSpace(it.Subtitle),
				URL:         strings.TrimSpace(it.URL),
				OpenMode:    "new_tab",
			}
			if it.Target != "" && it.Target != "_blank" {
				svc.OpenMode = "same_tab"
			}
			if it.Tag != "" {
				svc.Tags = append(svc.Tags, it.Tag)
			}
			svc.Tags = append(svc.Tags, strings.Fields(it.Keywords)...)
			if it.Type != "" {
				warns = append(warns, Warning{ipath + ".type", "smart card «" + it.Type + "» не переносится; ссылка импортирована как обычная карточка"})
			}
			svc.Normalize()
			if err := svc.Validate(); err != nil {
				warns = append(warns, Warning{ipath, "пропущено: " + err.Error()})
				continue
			}
			id := model.NewID("svc")
			if it.Logo != "" {
				if f := assets.find(it.Logo); f != nil {
					plan.icons[id] = f
				} else {
					warns = append(warns, Warning{ipath + ".logo", "файл " + it.Logo + " не найден в архиве иконок"})
				}
			}
			snap.Services = append(snap.Services, model.Service{ID: id, ServiceInput: svc})
			cfgJSON, _ := json.Marshal(model.LinkConfig{ServiceID: id})
			n := ii
			page.Widgets = append(page.Widgets, model.Widget{
				ID: fmt.Sprintf("new_w%d_%d", gi, ii), GroupID: gid, Type: model.WidgetLink, Config: cfgJSON,
				Layout: model.Layout{
					LG: &model.Rect{X: (n % 4) * 3, Y: n / 4, W: 3, H: 1},
					MD: &model.Rect{X: (n % 2) * 3, Y: n / 2, W: 3, H: 1},
					SM: &model.Rect{X: 0, Y: n, W: 1, H: 1},
				},
			})
		}
	}
	if len(page.Groups) == 0 {
		page.Groups = []model.Group{{ID: "new_g0", Title: ""}}
	}
	page.Normalize()
	if err := page.Validate(); err != nil {
		return homerPlan{}, warns, err
	}
	snap.Pages = []model.Page{page}
	snap.Settings.StartPageID = &page.ID
	plan.snap = snap
	return plan, warns, nil
}

func (p *homerPlan) materialize(ctx context.Context, saver assetSaver) ([]Warning, error) {
	assets, err := openHomerAssets(p.zip)
	if err != nil {
		return nil, err
	}
	var warns []Warning
	save := func(f *zip.File) (string, error) {
		data, err := assets.read(f)
		if err != nil {
			return "", err
		}
		a, err := saver.SaveBytes(ctx, data)
		if err != nil {
			return "", err
		}
		return a.ID, nil
	}
	for i := range p.snap.Services {
		s := &p.snap.Services[i]
		f, ok := p.icons[s.ID]
		if !ok {
			continue
		}
		id, err := save(f)
		if err != nil {
			warns = append(warns, Warning{"icon:" + f.Name, "иконка не загружена: " + err.Error()})
			continue
		}
		s.Icon = "asset:" + id
	}
	if p.logo != nil {
		id, err := save(p.logo)
		if err != nil {
			warns = append(warns, Warning{"logo", "логотип не загружен: " + err.Error()})
		} else {
			p.snap.Settings.LogoAssetID = &id
		}
	}
	return warns, nil
}

func mapValue(n *yaml.Node, key string) *yaml.Node {
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func reportUnknown(n *yaml.Node, prefix string, known map[string]bool, warns *[]Warning) {
	if n.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i].Value
		if known[k] {
			continue
		}
		p := k
		if prefix != "" {
			p = prefix + "." + k
		}
		msg, ok := homerIgnored[k]
		if !ok {
			msg = "неизвестное поле не переносится"
		}
		*warns = append(*warns, Warning{p, msg})
	}
}
