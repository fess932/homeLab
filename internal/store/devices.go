package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/fess932/homeLab/internal/model"
)

const deviceCols = "id, name, kind, address, interval_s, timeout_s, labels, secret_id, config, enabled, revision"

// deviceConfig — настройки драйвера; хранятся одним JSON, чтобы новый драйвер не требовал миграции.
type deviceConfig struct {
	Tuya     *model.TuyaConfig     `json:"tuya,omitempty"`
	HTTPJSON *model.HTTPJSONConfig `json:"http_json,omitempty"`
}

func scanDevice(sc interface{ Scan(...any) error }) (model.Device, error) {
	var d model.Device
	var labels, config string
	var secret sql.NullString
	var enabled bool
	if err := sc.Scan(&d.ID, &d.Name, &d.Kind, &d.Address, &d.IntervalS, &d.TimeoutS, &labels, &secret, &config, &enabled, &d.Revision); err != nil {
		return d, err
	}
	d.SecretID = strPtr(secret)
	d.Enabled = &enabled
	if err := json.Unmarshal([]byte(labels), &d.Labels); err != nil {
		return d, err
	}
	var c deviceConfig
	if err := json.Unmarshal([]byte(config), &c); err != nil {
		return d, err
	}
	d.Tuya, d.HTTPJSON = c.Tuya, c.HTTPJSON
	return d, nil
}

func deviceConfigJSON(in model.DeviceInput) string {
	return mustJSON(deviceConfig{Tuya: in.Tuya, HTTPJSON: in.HTTPJSON})
}

func (s *Store) ListDevices(ctx context.Context) ([]model.Device, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+deviceCols+" FROM devices ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Device{}
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) GetDevice(ctx context.Context, id string) (model.Device, error) {
	d, err := scanDevice(s.db.QueryRowContext(ctx, "SELECT "+deviceCols+" FROM devices WHERE id = ?", id))
	return d, notFound(err)
}

func (s *Store) CreateDevice(ctx context.Context, in model.DeviceInput) (model.Device, error) {
	id := model.NewID("dev")
	_, err := s.db.ExecContext(ctx, "INSERT INTO devices ("+deviceCols+", created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)",
		id, in.Name, in.Kind, in.Address, in.IntervalS, in.TimeoutS, mustJSON(in.Labels), nullStr(in.SecretID), deviceConfigJSON(in), *in.Enabled, unix(s.now()))
	if isFK(err) {
		return model.Device{}, model.Invalid("secret_id", "секрет не найден")
	}
	if err != nil {
		return model.Device{}, err
	}
	return s.GetDevice(ctx, id)
}

func (s *Store) UpdateDevice(ctx context.Context, id string, rev int64, in model.DeviceInput) (model.Device, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkRevision(ctx, tx, "devices", id, rev); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE devices SET name = ?, kind = ?, address = ?, interval_s = ?, timeout_s = ?, labels = ?, secret_id = ?, config = ?, enabled = ?, revision = revision + 1 WHERE id = ?",
			in.Name, in.Kind, in.Address, in.IntervalS, in.TimeoutS, mustJSON(in.Labels), nullStr(in.SecretID), deviceConfigJSON(in), *in.Enabled, id)
		if isFK(err) {
			return model.Invalid("secret_id", "секрет не найден")
		}
		return err
	})
	if err != nil {
		return model.Device{}, err
	}
	return s.GetDevice(ctx, id)
}

func (s *Store) DeleteDevice(ctx context.Context, id string) error {
	return deleteByID(ctx, s.db, "devices", id)
}
