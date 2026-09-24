package api

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/fess932/homeLab/internal/assets"
	"github.com/fess932/homeLab/internal/model"
)

const (
	maxImportYAML = 2 << 20
	maxImportZip  = 32 << 20
)

func (s *Server) listAssets(w http.ResponseWriter, r *http.Request) error {
	list, err := s.Store.ListAssets(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, list)
	return nil
}

func (s *Server) uploadAsset(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, assets.MaxSize+64<<10)
	file, _, err := r.FormFile("file")
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return &Error{Status: http.StatusRequestEntityTooLarge, Code: "payload_too_large", Message: assets.ErrTooLarge.Error()}
		}
		return &Error{Status: http.StatusBadRequest, Code: "bad_request", Message: "ожидается multipart-поле file"}
	}
	defer file.Close()
	a, err := s.Assets.Save(r.Context(), file)
	switch {
	case errors.Is(err, assets.ErrTooLarge):
		return &Error{Status: http.StatusRequestEntityTooLarge, Code: "payload_too_large", Message: err.Error()}
	case errors.Is(err, assets.ErrUnsupported):
		return &Error{Status: http.StatusUnsupportedMediaType, Code: "unsupported_media", Message: err.Error()}
	case err != nil:
		return err
	}
	writeJSON(w, http.StatusCreated, a)
	return nil
}

func (s *Server) deleteAsset(w http.ResponseWriter, r *http.Request) error {
	err := s.Assets.Delete(r.Context(), r.PathValue("id"))
	if errors.Is(err, model.ErrInUse) {
		return &Error{Status: http.StatusConflict, Code: "in_use", Message: "файл используется иконкой, логотипом или фоном"}
	}
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request) error {
	a, err := s.Store.GetAsset(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	f, err := s.Assets.Open(a)
	if err != nil {
		return err
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Type", a.MediaType)
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("ETag", `"`+a.Checksum+`"`)
	http.ServeContent(w, r, "", a.CreatedAt.Time, f)
	return nil
}

func readPart(r *http.Request, name string, limit int64) ([]byte, error) {
	f, _, err := r.FormFile(name)
	if errors.Is(err, http.ErrMissingFile) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, &Error{Status: http.StatusRequestEntityTooLarge, Code: "payload_too_large", Message: "файл " + name + " слишком большой"}
	}
	return data, nil
}

func (s *Server) importPreview(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportYAML+maxImportZip+1<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		return &Error{Status: http.StatusBadRequest, Code: "bad_request", Message: "ожидается multipart/form-data с полями format и file"}
	}
	defer r.MultipartForm.RemoveAll()
	data, err := readPart(r, "file", maxImportYAML)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return model.Invalid("file", "выберите файл конфигурации")
	}
	zipData, err := readPart(r, "assets", maxImportZip)
	if err != nil {
		return err
	}
	p, err := s.Importer.Preview(r.Context(), r.FormValue("format"), data, zipData)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, p)
	return nil
}

func (s *Server) importApply(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Token string `json:"token"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	p, err := s.Importer.Apply(r.Context(), in.Token)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, p)
	return nil
}

func (s *Server) export(w http.ResponseWriter, r *http.Request) error {
	data, err := s.Importer.Export(r.Context())
	if err != nil {
		return err
	}
	name := "homedeck-" + time.Now().UTC().Format("20060102-150405") + ".yaml"
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, err = w.Write(data)
	return err
}

type publicView struct {
	Title       string          `json:"title"`
	LogoAssetID *string         `json:"logo_asset_id"`
	Page        model.Page      `json:"page"`
	Services    []model.Service `json:"services"`
	// Presets — шаблоны запросов виджетов страницы: подписи, единицы и пороги, без выражений.
	Presets []model.Preset `json:"presets"`
}

func (s *Server) publicPageData(r *http.Request) (model.Page, error) {
	// Публикуется страница целиком: все её группы и виджеты. Закрытая страница
	// неотличима от несуществующей.
	p, err := s.Store.GetPage(r.Context(), r.PathValue("slug"))
	if err == nil && !p.Public {
		err = model.ErrNotFound
	}
	return p, err
}

func (s *Server) publicPage(w http.ResponseWriter, r *http.Request) error {
	p, err := s.publicPageData(r)
	if err != nil {
		return err
	}
	st, err := s.Store.GetSettings(r.Context())
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, wg := range p.Widgets {
		for _, id := range wg.ServiceIDs() {
			ids[id] = true
		}
	}
	all, err := s.Store.ListServices(r.Context())
	if err != nil {
		return err
	}
	services := []model.Service{}
	for _, sv := range all {
		if ids[sv.ID] {
			s.withStatus(&sv)
			// Аноним видит состояние, но не текст ошибки: в нём адреса и порты домашней сети.
			if sv.Status != nil {
				sv.Status.Error = ""
			}
			sv.SourceID = nil
			services = append(services, sv)
		}
	}
	presets := []model.Preset{}
	seen := map[string]bool{}
	for _, wg := range p.Widgets {
		m := wg.MetricRef()
		if m == nil || m.PresetID == "" || seen[m.PresetID] {
			continue
		}
		seen[m.PresetID] = true
		if pr, err := s.preset(r, m.PresetID); err == nil {
			pr.Expression = ""
			presets = append(presets, pr)
		}
	}
	writeJSON(w, http.StatusOK, publicView{Title: st.Title, LogoAssetID: st.LogoAssetID, Page: p, Services: services, Presets: presets})
	return nil
}

func (s *Server) publicWidgetData(w http.ResponseWriter, r *http.Request) error {
	p, err := s.publicPageData(r)
	if err != nil {
		return err
	}
	id := r.PathValue("id")
	for _, wg := range p.Widgets {
		if wg.ID != id {
			continue
		}
		rangeName := r.URL.Query().Get("range")
		if rangeName != "" {
			if _, ok := model.RangeDuration(rangeName); !ok {
				return model.Invalid("range", "1h, 6h, 24h, 7d или 30d")
			}
		}
		points := min(max(atoi(r.URL.Query().Get("points")), 0), 1000)
		data, err := s.buildWidgetData(r, wg, rangeName, time.Time{}, time.Time{}, points)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, data)
		return nil
	}
	return model.ErrNotFound
}

func (s *Server) spa() http.Handler {
	if s.UI == nil {
		return http.NotFoundHandler()
	}
	index, err := fs.ReadFile(s.UI, "index.html")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "UI не собран: выполните just web", http.StatusServiceUnavailable)
		})
	}
	files := http.FileServerFS(s.UI)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" && name != "index.html" {
			if st, err := fs.Stat(s.UI, name); err == nil && !st.IsDir() {
				if strings.HasPrefix(name, "static/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				files.ServeHTTP(w, r)
				return
			}
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
	})
}
