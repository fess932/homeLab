package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/fess932/homeLab/drivers"
	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/secrets"
)

// deviceBody — устройство и, по желанию, новые учётные данные в том же запросе:
// так ключ устройства сохраняется вместе с ним, а проверка работает до сохранения.
type deviceBody struct {
	model.DeviceInput
	Secret *model.SecretInput `json:"secret"`
}

func (s *Server) withDeviceStatus(d *model.Device) {
	d.Status = s.Devices.Status(d.ID)
}

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) error {
	list, err := s.Store.ListDevices(r.Context())
	if err != nil {
		return err
	}
	for i := range list {
		s.withDeviceStatus(&list[i])
	}
	writeJSON(w, http.StatusOK, list)
	return nil
}

func (s *Server) getDevice(w http.ResponseWriter, r *http.Request) error {
	d, err := s.Store.GetDevice(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	s.withDeviceStatus(&d)
	etag(w, d.Revision)
	writeJSON(w, http.StatusOK, d)
	return nil
}

// readDevice читает устройство и проверяет общие поля, настройки драйвера и тип
// учётных данных. Если учётные данные переданы в запросе, secret_id не требуется.
func (s *Server) readDevice(r *http.Request) (model.DeviceInput, *model.SecretInput, error) {
	var body deviceBody
	if err := decode(r, &body); err != nil {
		return body.DeviceInput, nil, err
	}
	in := body.DeviceInput
	in.Normalize()
	if err := in.Validate(); err != nil {
		return in, nil, err
	}
	drv, ok := drivers.Get(in.Kind)
	if !ok {
		return in, nil, model.Invalid("kind", "неизвестный тип устройства")
	}
	kinds := drv.Info().SecretKinds
	if body.Secret != nil {
		sec := *body.Secret
		if !slices.Contains(kinds, sec.Kind) {
			return in, nil, model.Invalid("secret.kind", "тип учётных данных не подходит устройству")
		}
		if strings.TrimSpace(sec.Name) == "" {
			sec.Name = in.Name
		}
		if err := sec.Validate(); err != nil {
			return in, nil, err
		}
		// Секрет ещё не сохранён: проверка драйвера не должна требовать secret_id.
		placeholder := "inline"
		in.SecretID = &placeholder
		err := drivers.Validate(&in)
		in.SecretID = nil
		return in, &sec, err
	}
	if err := drivers.Validate(&in); err != nil {
		return in, nil, err
	}
	return in, nil, s.checkSecretKind(r.Context(), in.SecretID, kinds...)
}

// checkSecretKind не даёт привязать секрет не того типа: ключ устройства не должен
// уйти в заголовок Authorization, а логин с паролем — в шифрование Tuya.
func (s *Server) checkSecretKind(ctx context.Context, id *string, kinds ...string) error {
	if id == nil {
		return nil
	}
	rec, err := s.Store.GetSecret(ctx, *id)
	if errors.Is(err, model.ErrNotFound) {
		return model.Invalid("secret_id", "секрет не найден")
	}
	if err != nil {
		return err
	}
	if !slices.Contains(kinds, rec.Kind) {
		if slices.Contains(kinds, model.SecretKey) {
			return model.Invalid("secret_id", "нужен секрет типа «ключ устройства»")
		}
		return model.Invalid("secret_id", "нужен секрет с логином и паролем или токеном")
	}
	return nil
}

func payloadOf(in model.SecretInput) secrets.Payload {
	switch in.Kind {
	case "bearer":
		return secrets.Payload{Token: in.Token}
	case model.SecretKey:
		return secrets.Payload{Key: in.Key}
	}
	return secrets.Payload{Username: in.Username, Password: in.Password}
}

// saveSecret шифрует и сохраняет новые учётные данные.
func (s *Server) saveSecret(ctx context.Context, name, kind, mask string, p secrets.Payload) (model.Secret, error) {
	id := model.NewID("sec")
	sealed, err := s.Box.Seal(id, p)
	if err != nil {
		return model.Secret{}, err
	}
	return s.Store.CreateSecret(ctx, id, name, kind, mask, sealed, secrets.KeyVersion)
}

// storeDevice создаёт или обновляет устройство; новые учётные данные из запроса
// сохраняются первыми и удаляются, если устройство сохранить не удалось.
func (s *Server) storeDevice(ctx context.Context, id string, rev int64, in model.DeviceInput, sec *model.SecretInput) (model.Device, error) {
	var created *model.Secret
	if sec != nil {
		c, err := s.saveSecret(ctx, sec.Name, sec.Kind, sec.Mask(), payloadOf(*sec))
		if err != nil {
			return model.Device{}, err
		}
		created, in.SecretID = &c, &c.ID
	}
	var d model.Device
	var err error
	if id == "" {
		d, err = s.Store.CreateDevice(ctx, in)
	} else {
		d, err = s.Store.UpdateDevice(ctx, id, rev, in)
	}
	if err != nil {
		if created != nil {
			_ = s.Store.DeleteSecret(ctx, created.ID)
		}
		return model.Device{}, err
	}
	s.OnDevices()
	s.withDeviceStatus(&d)
	return d, nil
}

func (s *Server) createDevice(w http.ResponseWriter, r *http.Request) error {
	in, sec, err := s.readDevice(r)
	if err != nil {
		return err
	}
	d, err := s.storeDevice(r.Context(), "", 0, in, sec)
	if err != nil {
		return err
	}
	etag(w, d.Revision)
	writeJSON(w, http.StatusCreated, d)
	return nil
}

func (s *Server) updateDevice(w http.ResponseWriter, r *http.Request) error {
	rev, err := ifMatch(r)
	if err != nil {
		return err
	}
	in, sec, err := s.readDevice(r)
	if err != nil {
		return err
	}
	d, err := s.storeDevice(r.Context(), r.PathValue("id"), rev, in, sec)
	if err != nil {
		return err
	}
	etag(w, d.Revision)
	writeJSON(w, http.StatusOK, d)
	return nil
}

func (s *Server) deleteDevice(w http.ResponseWriter, r *http.Request) error {
	if err := s.Store.DeleteDevice(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	s.OnDevices()
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// testDevice опрашивает черновик устройства один раз, не сохраняя ни его, ни учётные данные.
func (s *Server) testDevice(w http.ResponseWriter, r *http.Request) error {
	in, sec, err := s.readDevice(r)
	if err != nil {
		return err
	}
	var inline *secrets.Payload
	if sec != nil {
		p := payloadOf(*sec)
		inline = &p
	}
	writeJSON(w, http.StatusOK, s.Devices.Test(r.Context(), in, inline))
	return nil
}

func (s *Server) listDrivers(w http.ResponseWriter, _ *http.Request) error {
	out := []drivers.Info{}
	for _, d := range drivers.All() {
		out = append(out, d.Info())
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func driverOf(r *http.Request) (drivers.Driver, error) {
	d, ok := drivers.Get(r.PathValue("kind"))
	if !ok {
		return nil, model.ErrNotFound
	}
	return d, nil
}

func accountsOf[T any](d drivers.Driver) (T, error) {
	v, ok := d.(T)
	if !ok {
		return v, &Error{Status: http.StatusNotFound, Code: "not_found", Message: "драйвер этого не умеет"}
	}
	return v, nil
}

type accountRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// accounts возвращает подключённые аккаунты драйвера с расшифрованными данными.
func (s *Server) accounts(ctx context.Context, kind string) ([]accountRef, map[string]json.RawMessage, error) {
	list, err := s.Store.ListSecrets(ctx)
	if err != nil {
		return nil, nil, err
	}
	refs := []accountRef{}
	data := map[string]json.RawMessage{}
	for _, sec := range list {
		if sec.Kind != drivers.AccountKind(kind) {
			continue
		}
		rec, err := s.Store.GetSecret(ctx, sec.ID)
		if err != nil {
			return nil, nil, err
		}
		p, err := s.Box.Open(rec.ID, rec.Payload)
		if err != nil {
			continue
		}
		refs = append(refs, accountRef{ID: sec.ID, Name: sec.Name})
		data[sec.ID] = p.Data
	}
	return refs, data, nil
}

// saveAccountData сохраняет обновлённые токены аккаунта, если драйвер их вернул.
func (s *Server) saveAccountData(ctx context.Context, id string, before, after json.RawMessage) {
	if len(after) == 0 || string(after) == string(before) {
		return
	}
	sealed, err := s.Box.Seal(id, secrets.Payload{Data: after})
	if err == nil {
		err = s.Store.ReplaceSecretPayload(ctx, id, sealed, secrets.KeyVersion)
	}
	if err != nil {
		s.Log.Warn("save account tokens", "secret", id, "err", err)
	}
}

type discoverResponse struct {
	drivers.DiscoverResult
	Accounts []accountRef `json:"accounts"`
	// Added — id уже добавленных устройств HomeDeck по Candidate.Ref.
	Added map[string]string `json:"added"`
}

// discoverDevices объединяет поиск в локальной сети и устройства подключённых аккаунтов:
// из сети известен адрес, из аккаунта — имя, ключ и описание точек данных.
func (s *Server) discoverDevices(w http.ResponseWriter, r *http.Request) error {
	drv, err := driverOf(r)
	if err != nil {
		return err
	}
	var body struct {
		Subnet string `json:"subnet"`
	}
	if r.ContentLength != 0 {
		if err := decode(r, &body); err != nil {
			return err
		}
	}
	res := discoverResponse{Candidates: []drivers.Candidate{}, Subnets: []string{}, Warnings: []string{}, Accounts: []accountRef{}, Added: map[string]string{}}
	byRef := map[string]int{}
	if disc, ok := drv.(drivers.Discoverer); ok {
		lan, err := disc.Discover(r.Context(), drivers.DiscoverOptions{Subnet: body.Subnet})
		if err != nil {
			return err
		}
		res.Subnets, res.Warnings = lan.Subnets, lan.Warnings
		for _, c := range lan.Candidates {
			if c.Ref != "" {
				byRef[c.Ref] = len(res.Candidates)
			}
			res.Candidates = append(res.Candidates, c)
		}
	}
	if acc, ok := drv.(drivers.AccountProvider); ok {
		refs, data, err := s.accounts(r.Context(), drv.Info().Kind)
		if err != nil {
			return err
		}
		res.Accounts = refs
		for _, a := range refs {
			list, updated, err := acc.ListDevices(r.Context(), data[a.ID])
			if err != nil {
				_, msg := drivers.Classify(err)
				res.Warnings = append(res.Warnings, a.Name+": "+msg)
				continue
			}
			s.saveAccountData(r.Context(), a.ID, data[a.ID], updated)
			for _, c := range list {
				c.AccountID = a.ID
				if i, seen := byRef[c.Ref]; seen {
					// Устройство видно и в сети, и в аккаунте: адрес из сети, остальное из аккаунта.
					c.Address, c.InNetwork = res.Candidates[i].Address, true
					res.Candidates[i] = c
					continue
				}
				byRef[c.Ref] = len(res.Candidates)
				res.Candidates = append(res.Candidates, c)
			}
		}
	}
	if ident, ok := drv.(drivers.Identifier); ok {
		existing, err := s.Store.ListDevices(r.Context())
		if err != nil {
			return err
		}
		for _, d := range existing {
			if d.Kind == drv.Info().Kind {
				if ref := ident.Identity(d.Config); ref != "" {
					res.Added[ref] = d.ID
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}

func (s *Server) startLogin(w http.ResponseWriter, r *http.Request) error {
	drv, err := driverOf(r)
	if err != nil {
		return err
	}
	acc, err := accountsOf[drivers.AccountProvider](drv)
	if err != nil {
		return err
	}
	var params map[string]string
	if err := decode(r, &params); err != nil {
		return err
	}
	login, err := acc.StartLogin(r.Context(), params)
	if err != nil {
		return cloudError(err)
	}
	writeJSON(w, http.StatusOK, login)
	return nil
}

// checkLogin проверяет, подтверждён ли вход; после подтверждения аккаунт
// сохраняется зашифрованным секретом.
func (s *Server) checkLogin(w http.ResponseWriter, r *http.Request) error {
	drv, err := driverOf(r)
	if err != nil {
		return err
	}
	acc, err := accountsOf[drivers.AccountProvider](drv)
	if err != nil {
		return err
	}
	done, account, err := acc.CheckLogin(r.Context(), r.PathValue("id"))
	if err != nil {
		return cloudError(err)
	}
	if !done {
		writeJSON(w, http.StatusOK, map[string]any{"state": "pending"})
		return nil
	}
	sec, err := s.saveSecret(r.Context(), account.Name, drivers.AccountKind(drv.Info().Kind), "••••••", secrets.Payload{Data: account.Data})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": "done", "account": sec})
	return nil
}

// adoptDevice добавляет устройство из аккаунта: ключ сервер получает из облака сам
// и сохраняет учётными данными, в браузер он не попадает.
func (s *Server) adoptDevice(w http.ResponseWriter, r *http.Request) error {
	drv, err := driverOf(r)
	if err != nil {
		return err
	}
	acc, err := accountsOf[drivers.AccountProvider](drv)
	if err != nil {
		return err
	}
	var body struct {
		AccountID string `json:"account_id"`
		Ref       string `json:"ref"`
		Name      string `json:"name"`
		Address   string `json:"address"`
	}
	if err := decode(r, &body); err != nil {
		return err
	}
	_, data, err := s.accounts(r.Context(), drv.Info().Kind)
	if err != nil {
		return err
	}
	raw, ok := data[body.AccountID]
	if !ok {
		return model.Invalid("account_id", "аккаунт не найден")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	adopted, updated, err := acc.Adopt(ctx, raw, body.Ref)
	if err != nil {
		return cloudError(err)
	}
	s.saveAccountData(r.Context(), body.AccountID, raw, updated)
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = adopted.Candidate.Name
	}
	in := model.DeviceInput{Name: name, Kind: drv.Info().Kind, Address: body.Address, Config: adopted.Candidate.Config}
	in.Normalize()
	if err := in.Validate(); err != nil {
		return err
	}
	sec := &model.SecretInput{Name: name + ": ключ", Kind: model.SecretKey, Key: adopted.Key}
	placeholder := "inline"
	in.SecretID = &placeholder
	if err := drivers.Validate(&in); err != nil {
		return err
	}
	in.SecretID = nil
	d, err := s.storeDevice(r.Context(), "", 0, in, sec)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, d)
	return nil
}

// cloudError показывает ошибки облака как 502: запрос корректен, не ответил внешний сервис.
func cloudError(err error) error {
	if e := (*drivers.Error)(nil); errors.As(err, &e) {
		return &Error{Status: http.StatusBadGateway, Code: "driver_" + e.Kind, Message: e.Msg}
	}
	if _, ok := errors.AsType[*model.ValidationError](err); ok {
		return err
	}
	_, msg := drivers.Classify(err)
	return &Error{Status: http.StatusBadGateway, Code: "driver_connect", Message: msg}
}
