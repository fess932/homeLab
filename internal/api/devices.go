package api

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/secrets"
)

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

// deviceInput читает и проверяет устройство, включая тип привязанного секрета.
func (s *Server) deviceInput(r *http.Request) (model.DeviceInput, error) {
	var in model.DeviceInput
	if err := decode(r, &in); err != nil {
		return in, err
	}
	in.Normalize()
	if err := in.Validate(); err != nil {
		return in, err
	}
	return in, s.checkSecretKind(r.Context(), in.SecretID, deviceSecretKinds(in)...)
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

func (s *Server) createDevice(w http.ResponseWriter, r *http.Request) error {
	in, err := s.deviceInput(r)
	if err != nil {
		return err
	}
	d, err := s.Store.CreateDevice(r.Context(), in)
	if err != nil {
		return err
	}
	s.OnDevices()
	s.withDeviceStatus(&d)
	etag(w, d.Revision)
	writeJSON(w, http.StatusCreated, d)
	return nil
}

func (s *Server) updateDevice(w http.ResponseWriter, r *http.Request) error {
	rev, err := ifMatch(r)
	if err != nil {
		return err
	}
	in, err := s.deviceInput(r)
	if err != nil {
		return err
	}
	d, err := s.Store.UpdateDevice(r.Context(), r.PathValue("id"), rev, in)
	if err != nil {
		return err
	}
	s.OnDevices()
	s.withDeviceStatus(&d)
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

// testDevice опрашивает черновик устройства один раз, не сохраняя его. Ключ можно
// передать прямо в запросе (поле secret): так новое устройство проверяется до того,
// как его ключ сохранён в учётные данные.
func (s *Server) testDevice(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		model.DeviceInput
		Secret *model.SecretInput `json:"secret"`
	}
	if err := decode(r, &body); err != nil {
		return err
	}
	in := body.DeviceInput
	in.Normalize()
	if body.Secret == nil {
		if err := in.Validate(); err != nil {
			return err
		}
		if err := s.checkSecretKind(r.Context(), in.SecretID, deviceSecretKinds(in)...); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, s.Devices.Test(r.Context(), in, nil))
		return nil
	}
	sec := *body.Secret
	sec.Name = "проверка"
	if !slices.Contains(deviceSecretKinds(in), sec.Kind) {
		return model.Invalid("secret.kind", "тип учётных данных не подходит устройству")
	}
	if err := sec.Validate(); err != nil {
		return err
	}
	placeholder := "inline"
	in.SecretID = &placeholder
	if err := in.Validate(); err != nil {
		return err
	}
	p := secrets.Payload{Username: sec.Username, Password: sec.Password, Token: sec.Token, Key: sec.Key}
	writeJSON(w, http.StatusOK, s.Devices.Test(r.Context(), in, &p))
	return nil
}

func deviceSecretKinds(in model.DeviceInput) []string {
	if in.Kind == model.DeviceTuya {
		return []string{model.SecretKey}
	}
	return []string{"basic", "bearer"}
}
