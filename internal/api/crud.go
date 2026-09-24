package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/secrets"
)

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) error {
	st, err := s.Store.GetSettings(r.Context())
	if err != nil {
		return err
	}
	s.decorateSettings(r, &st)
	etag(w, st.Revision)
	writeJSON(w, http.StatusOK, st)
	return nil
}

func (s *Server) decorateSettings(_ *http.Request, st *model.Settings) {
	st.Runtime = &model.RuntimeInfo{
		Listen:      s.Config.Listen,
		DataDir:     s.Config.DataDir,
		Retention:   s.Config.Retention,
		Version:     s.Config.Version,
		TSDBVersion: s.tsdbVersion(),
	}
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) error {
	rev, err := ifMatch(r)
	if err != nil {
		return err
	}
	var in model.Settings
	if err := decode(r, &in); err != nil {
		return err
	}
	if err := in.Validate(); err != nil {
		return err
	}
	st, err := s.Store.UpdateSettings(r.Context(), rev, in)
	if err != nil {
		return err
	}
	s.decorateSettings(r, &st)
	etag(w, st.Revision)
	writeJSON(w, http.StatusOK, st)
	return nil
}

func (s *Server) listPages(w http.ResponseWriter, r *http.Request) error {
	pages, err := s.Store.ListPages(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, pages)
	return nil
}

func (s *Server) getPage(w http.ResponseWriter, r *http.Request) error {
	p, err := s.Store.GetPage(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	etag(w, p.Revision)
	writeJSON(w, http.StatusOK, p)
	return nil
}

func (s *Server) createPage(w http.ResponseWriter, r *http.Request) error {
	var in model.PageInput
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Normalize()
	if err := in.Validate(); err != nil {
		return err
	}
	p, err := s.Store.CreatePage(r.Context(), in)
	if err != nil {
		return err
	}
	etag(w, p.Revision)
	writeJSON(w, http.StatusCreated, p)
	return nil
}

func (s *Server) updatePage(w http.ResponseWriter, r *http.Request) error {
	rev, err := ifMatch(r)
	if err != nil {
		return err
	}
	var in model.PageInput
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Normalize()
	if err := in.Validate(); err != nil {
		return err
	}
	cur, err := s.Store.GetPage(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	p, err := s.Store.UpdatePage(r.Context(), cur.ID, rev, in)
	if err != nil {
		return err
	}
	etag(w, p.Revision)
	writeJSON(w, http.StatusOK, p)
	return nil
}

func (s *Server) deletePage(w http.ResponseWriter, r *http.Request) error {
	if err := s.Store.DeletePage(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) withStatus(sv *model.Service) {
	if sv.CheckID == nil {
		return
	}
	st, _ := s.Scheduler.Status(*sv.CheckID)
	sv.Status = &st
}

func (s *Server) listServices(w http.ResponseWriter, r *http.Request) error {
	list, err := s.Store.ListServices(r.Context())
	if err != nil {
		return err
	}
	for i := range list {
		s.withStatus(&list[i])
	}
	writeJSON(w, http.StatusOK, list)
	return nil
}

func (s *Server) getService(w http.ResponseWriter, r *http.Request) error {
	sv, err := s.Store.GetService(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	s.withStatus(&sv)
	etag(w, sv.Revision)
	writeJSON(w, http.StatusOK, sv)
	return nil
}

func (s *Server) createService(w http.ResponseWriter, r *http.Request) error {
	var in model.ServiceInput
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Normalize()
	if err := in.Validate(); err != nil {
		return err
	}
	sv, err := s.Store.CreateService(r.Context(), in)
	if err != nil {
		return err
	}
	etag(w, sv.Revision)
	writeJSON(w, http.StatusCreated, sv)
	return nil
}

func (s *Server) updateService(w http.ResponseWriter, r *http.Request) error {
	rev, err := ifMatch(r)
	if err != nil {
		return err
	}
	var in model.ServiceInput
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Normalize()
	if err := in.Validate(); err != nil {
		return err
	}
	sv, err := s.Store.UpdateService(r.Context(), r.PathValue("id"), rev, in)
	if err != nil {
		return err
	}
	s.withStatus(&sv)
	etag(w, sv.Revision)
	writeJSON(w, http.StatusOK, sv)
	return nil
}

func (s *Server) deleteService(w http.ResponseWriter, r *http.Request) error {
	if err := s.Store.DeleteService(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	s.OnChecks()
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) checkWithStatus(c *model.Check) {
	st, _ := s.Scheduler.Status(c.ID)
	c.Status = &st
}

func (s *Server) listChecks(w http.ResponseWriter, r *http.Request) error {
	list, err := s.Store.ListChecks(r.Context())
	if err != nil {
		return err
	}
	for i := range list {
		s.checkWithStatus(&list[i])
	}
	writeJSON(w, http.StatusOK, list)
	return nil
}

func (s *Server) getCheck(w http.ResponseWriter, r *http.Request) error {
	c, err := s.Store.GetCheck(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	s.checkWithStatus(&c)
	etag(w, c.Revision)
	writeJSON(w, http.StatusOK, c)
	return nil
}

func (s *Server) createCheck(w http.ResponseWriter, r *http.Request) error {
	var in model.CheckInput
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Normalize()
	if err := in.Validate(); err != nil {
		return err
	}
	c, err := s.Store.CreateCheck(r.Context(), in)
	if err != nil {
		return err
	}
	s.OnChecks()
	s.checkWithStatus(&c)
	etag(w, c.Revision)
	writeJSON(w, http.StatusCreated, c)
	return nil
}

func (s *Server) updateCheck(w http.ResponseWriter, r *http.Request) error {
	rev, err := ifMatch(r)
	if err != nil {
		return err
	}
	var in model.CheckInput
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Normalize()
	if err := in.Validate(); err != nil {
		return err
	}
	c, err := s.Store.UpdateCheck(r.Context(), r.PathValue("id"), rev, in)
	if err != nil {
		return err
	}
	s.OnChecks()
	s.checkWithStatus(&c)
	etag(w, c.Revision)
	writeJSON(w, http.StatusOK, c)
	return nil
}

func (s *Server) deleteCheck(w http.ResponseWriter, r *http.Request) error {
	if err := s.Store.DeleteCheck(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	s.OnChecks()
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func systemSources() []model.Source {
	mk := func(id, name, url string) model.Source {
		return model.Source{ID: id, System: true, Revision: 1,
			Name: name, Kind: model.SourcePrometheus, URL: url, IntervalS: 15, TimeoutS: 5,
			Labels: map[string]string{}, Enabled: new(true)}
	}
	return []model.Source{
		mk(model.SystemSourceApp, "HomeDeck", "http://127.0.0.1:9091/metrics"),
		mk(model.SystemSourceTSDB, "VictoriaMetrics", "http://127.0.0.1:8428/metrics"),
	}
}

func (s *Server) listSources(w http.ResponseWriter, r *http.Request) error {
	list, err := s.Store.ListSources(r.Context())
	if err != nil {
		return err
	}
	all := append(systemSources(), list...)
	for i := range all {
		all[i].Status = s.Watcher.Status(all[i])
	}
	writeJSON(w, http.StatusOK, all)
	return nil
}

func (s *Server) findSource(r *http.Request, id string) (model.Source, error) {
	for _, src := range systemSources() {
		if src.ID == id {
			return src, nil
		}
	}
	return s.Store.GetSource(r.Context(), id)
}

func (s *Server) getSource(w http.ResponseWriter, r *http.Request) error {
	src, err := s.findSource(r, r.PathValue("id"))
	if err != nil {
		return err
	}
	src.Status = s.Watcher.Status(src)
	etag(w, src.Revision)
	writeJSON(w, http.StatusOK, src)
	return nil
}

func isSystemSource(id string) bool {
	return id == model.SystemSourceApp || id == model.SystemSourceTSDB
}

func (s *Server) createSource(w http.ResponseWriter, r *http.Request) error {
	var in model.SourceInput
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Normalize()
	if err := in.Validate(); err != nil {
		return err
	}
	src, err := s.Store.CreateSource(r.Context(), in)
	if err != nil {
		return err
	}
	s.Reconciler.Kick()
	src.Status = s.Watcher.Status(src)
	etag(w, src.Revision)
	writeJSON(w, http.StatusCreated, src)
	return nil
}

func (s *Server) updateSource(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if isSystemSource(id) {
		return &Error{Status: http.StatusForbidden, Code: "forbidden", Message: "встроенный источник не редактируется"}
	}
	rev, err := ifMatch(r)
	if err != nil {
		return err
	}
	var in model.SourceInput
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Normalize()
	if err := in.Validate(); err != nil {
		return err
	}
	src, err := s.Store.UpdateSource(r.Context(), id, rev, in)
	if err != nil {
		return err
	}
	s.Reconciler.Kick()
	src.Status = s.Watcher.Status(src)
	etag(w, src.Revision)
	writeJSON(w, http.StatusOK, src)
	return nil
}

func (s *Server) deleteSource(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if isSystemSource(id) {
		return &Error{Status: http.StatusForbidden, Code: "forbidden", Message: "встроенный источник не удаляется"}
	}
	if err := s.Store.DeleteSource(r.Context(), id); err != nil {
		return err
	}
	s.Reconciler.Kick()
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) listSecrets(w http.ResponseWriter, r *http.Request) error {
	list, err := s.Store.ListSecrets(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, list)
	return nil
}

func (s *Server) sealSecret(id string, in model.SecretInput) ([]byte, error) {
	p := secrets.Payload{Username: in.Username, Password: in.Password}
	if in.Kind == "bearer" {
		p = secrets.Payload{Token: in.Token}
	}
	return s.Box.Seal(id, p)
}

func (s *Server) createSecret(w http.ResponseWriter, r *http.Request) error {
	var in model.SecretInput
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Name = strings.TrimSpace(in.Name)
	if err := in.Validate(); err != nil {
		return err
	}
	id := model.NewID("sec")
	sealed, err := s.sealSecret(id, in)
	if err != nil {
		return err
	}
	sec, err := s.Store.CreateSecret(r.Context(), id, in.Name, in.Kind, in.Mask(), sealed, secrets.KeyVersion)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, sec)
	return nil
}

func (s *Server) updateSecret(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	var in model.SecretInput
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Name = strings.TrimSpace(in.Name)
	if err := in.Validate(); err != nil {
		return err
	}
	sealed, err := s.sealSecret(id, in)
	if err != nil {
		return err
	}
	if err := s.Store.UpdateSecret(r.Context(), id, in.Name, in.Kind, in.Mask(), sealed, secrets.KeyVersion); err != nil {
		return err
	}
	s.Reconciler.Kick()
	list, err := s.Store.ListSecrets(r.Context())
	if err != nil {
		return err
	}
	for _, sec := range list {
		if sec.ID == id {
			writeJSON(w, http.StatusOK, sec)
			return nil
		}
	}
	return model.ErrNotFound
}

func (s *Server) deleteSecret(w http.ResponseWriter, r *http.Request) error {
	err := s.Store.DeleteSecret(r.Context(), r.PathValue("id"))
	if errors.Is(err, model.ErrInUse) {
		return &Error{Status: http.StatusConflict, Code: "in_use", Message: "секрет используется источниками, сначала отвяжите его"}
	}
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) listPresets(w http.ResponseWriter, r *http.Request) error {
	custom, err := s.Store.ListPresets(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, append(model.BuiltinPresets(), custom...))
	return nil
}

func (s *Server) preset(r *http.Request, id string) (model.Preset, error) {
	if p, ok := model.BuiltinPreset(id); ok {
		return p, nil
	}
	if model.IsBuiltinPresetID(id) {
		return model.Preset{}, model.ErrNotFound
	}
	return s.Store.GetPreset(r.Context(), id)
}

func (s *Server) createPreset(w http.ResponseWriter, r *http.Request) error {
	var in model.PresetInput
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Normalize()
	if err := in.Validate(); err != nil {
		return err
	}
	p, err := s.Store.CreatePreset(r.Context(), in)
	if err != nil {
		return err
	}
	etag(w, p.Revision)
	writeJSON(w, http.StatusCreated, p)
	return nil
}

func (s *Server) updatePreset(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if model.IsBuiltinPresetID(id) {
		return &Error{Status: http.StatusForbidden, Code: "forbidden", Message: "встроенный шаблон не редактируется"}
	}
	rev, err := ifMatch(r)
	if err != nil {
		return err
	}
	var in model.PresetInput
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Normalize()
	if err := in.Validate(); err != nil {
		return err
	}
	p, err := s.Store.UpdatePreset(r.Context(), id, rev, in)
	if err != nil {
		return err
	}
	etag(w, p.Revision)
	writeJSON(w, http.StatusOK, p)
	return nil
}

func (s *Server) deletePreset(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if model.IsBuiltinPresetID(id) {
		return &Error{Status: http.StatusForbidden, Code: "forbidden", Message: "встроенный шаблон не удаляется"}
	}
	if err := s.Store.DeletePreset(r.Context(), id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) listRevisions(w http.ResponseWriter, r *http.Request) error {
	list, err := s.Store.ListRevisions(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, list)
	return nil
}
