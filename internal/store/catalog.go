package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/fess932/homeLab/internal/model"
)

func timeUnix(v int64) time.Time { return time.Unix(v, 0) }

const serviceCols = "s.id, s.name, s.description, s.url, s.icon, s.tags, s.open_mode, s.source_id, s.revision, c.id"

func scanService(sc interface{ Scan(...any) error }) (model.Service, error) {
	var sv model.Service
	var tags string
	var src, chk sql.NullString
	if err := sc.Scan(&sv.ID, &sv.Name, &sv.Description, &sv.URL, &sv.Icon, &tags, &sv.OpenMode, &src, &sv.Revision, &chk); err != nil {
		return sv, err
	}
	sv.SourceID, sv.CheckID = strPtr(src), strPtr(chk)
	return sv, json.Unmarshal([]byte(tags), &sv.Tags)
}

func (s *Store) ListServices(ctx context.Context) ([]model.Service, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+serviceCols+" FROM services s LEFT JOIN checks c ON c.service_id = s.id ORDER BY s.name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Service{}
	for rows.Next() {
		sv, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sv)
	}
	return out, rows.Err()
}

func (s *Store) GetService(ctx context.Context, id string) (model.Service, error) {
	sv, err := scanService(s.db.QueryRowContext(ctx, "SELECT "+serviceCols+" FROM services s LEFT JOIN checks c ON c.service_id = s.id WHERE s.id = ?", id))
	return sv, notFound(err)
}

func (s *Store) CreateService(ctx context.Context, in model.ServiceInput) (model.Service, error) {
	id := model.NewID("svc")
	_, err := s.db.ExecContext(ctx, "INSERT INTO services (id, name, description, url, icon, tags, open_mode, source_id, revision, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?)",
		id, in.Name, in.Description, in.URL, in.Icon, mustJSON(in.Tags), in.OpenMode, nullStr(in.SourceID), unix(s.now()))
	if isFK(err) {
		return model.Service{}, model.Invalid("source_id", "источник не найден")
	}
	if err != nil {
		return model.Service{}, err
	}
	return s.GetService(ctx, id)
}

func (s *Store) UpdateService(ctx context.Context, id string, rev int64, in model.ServiceInput) (model.Service, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkRevision(ctx, tx, "services", id, rev); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE services SET name = ?, description = ?, url = ?, icon = ?, tags = ?, open_mode = ?, source_id = ?, revision = revision + 1 WHERE id = ?",
			in.Name, in.Description, in.URL, in.Icon, mustJSON(in.Tags), in.OpenMode, nullStr(in.SourceID), id)
		if isFK(err) {
			return model.Invalid("source_id", "источник не найден")
		}
		return err
	})
	if err != nil {
		return model.Service{}, err
	}
	return s.GetService(ctx, id)
}

func (s *Store) DeleteService(ctx context.Context, id string) error {
	return deleteByID(ctx, s.db, "services", id)
}

func deleteByID(ctx context.Context, q queryer, table, id string) error {
	res, err := q.ExecContext(ctx, "DELETE FROM "+table+" WHERE id = ?", id)
	if isFK(err) {
		return model.ErrInUse
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return model.ErrNotFound
	}
	return nil
}

const checkCols = "id, service_id, kind, target, expected_status, interval_s, timeout_s, enabled, ca_pem, revision"

func scanCheck(sc interface{ Scan(...any) error }) (model.Check, error) {
	var c model.Check
	var enabled bool
	err := sc.Scan(&c.ID, &c.ServiceID, &c.Kind, &c.Target, &c.ExpectedStatus, &c.IntervalS, &c.TimeoutS, &enabled, &c.CAPEM, &c.Revision)
	c.Enabled = &enabled
	return c, err
}

func (s *Store) ListChecks(ctx context.Context) ([]model.Check, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+checkCols+" FROM checks ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Check{}
	for rows.Next() {
		c, err := scanCheck(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetCheck(ctx context.Context, id string) (model.Check, error) {
	c, err := scanCheck(s.db.QueryRowContext(ctx, "SELECT "+checkCols+" FROM checks WHERE id = ?", id))
	return c, notFound(err)
}

func (s *Store) CreateCheck(ctx context.Context, in model.CheckInput) (model.Check, error) {
	id := model.NewID("chk")
	_, err := s.db.ExecContext(ctx, "INSERT INTO checks ("+checkCols+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1)",
		id, in.ServiceID, in.Kind, in.Target, in.ExpectedStatus, in.IntervalS, in.TimeoutS, *in.Enabled, in.CAPEM)
	switch {
	case isFK(err):
		return model.Check{}, model.Invalid("service_id", "сервис не найден")
	case isUnique(err):
		return model.Check{}, model.Invalid("service_id", "у сервиса уже есть проверка")
	case err != nil:
		return model.Check{}, err
	}
	return s.GetCheck(ctx, id)
}

func (s *Store) UpdateCheck(ctx context.Context, id string, rev int64, in model.CheckInput) (model.Check, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkRevision(ctx, tx, "checks", id, rev); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE checks SET service_id = ?, kind = ?, target = ?, expected_status = ?, interval_s = ?, timeout_s = ?, enabled = ?, ca_pem = ?, revision = revision + 1 WHERE id = ?",
			in.ServiceID, in.Kind, in.Target, in.ExpectedStatus, in.IntervalS, in.TimeoutS, *in.Enabled, in.CAPEM, id)
		switch {
		case isFK(err):
			return model.Invalid("service_id", "сервис не найден")
		case isUnique(err):
			return model.Invalid("service_id", "у сервиса уже есть проверка")
		}
		return err
	})
	if err != nil {
		return model.Check{}, err
	}
	return s.GetCheck(ctx, id)
}

func (s *Store) DeleteCheck(ctx context.Context, id string) error {
	return deleteByID(ctx, s.db, "checks", id)
}

const sourceCols = "id, name, kind, url, interval_s, timeout_s, labels, secret_id, tls, enabled, revision, last_attempt, last_success, last_error"

func scanSource(sc interface{ Scan(...any) error }) (model.Source, error) {
	var src model.Source
	var labels, tls string
	var secret sql.NullString
	var enabled bool
	var attempt, success sql.NullInt64
	if err := sc.Scan(&src.ID, &src.Name, &src.Kind, &src.URL, &src.IntervalS, &src.TimeoutS, &labels, &secret, &tls, &enabled, &src.Revision, &attempt, &success, &src.Status.Error); err != nil {
		return src, err
	}
	src.SecretID = strPtr(secret)
	src.Enabled = &enabled
	src.Status.LastAttempt, src.Status.LastSuccess = fromUnix(attempt), fromUnix(success)
	if err := json.Unmarshal([]byte(labels), &src.Labels); err != nil {
		return src, err
	}
	return src, json.Unmarshal([]byte(tls), &src.TLS)
}

func (s *Store) ListSources(ctx context.Context) ([]model.Source, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+sourceCols+" FROM sources ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Source{}
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

func (s *Store) GetSource(ctx context.Context, id string) (model.Source, error) {
	src, err := scanSource(s.db.QueryRowContext(ctx, "SELECT "+sourceCols+" FROM sources WHERE id = ?", id))
	return src, notFound(err)
}

func (s *Store) CreateSource(ctx context.Context, in model.SourceInput) (model.Source, error) {
	id := model.NewID("src")
	err := s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO sources (id, name, kind, url, interval_s, timeout_s, labels, secret_id, tls, enabled, revision, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)",
			id, in.Name, in.Kind, in.URL, in.IntervalS, in.TimeoutS, mustJSON(in.Labels), nullStr(in.SecretID), mustJSON(in.TLS), *in.Enabled, unix(s.now()))
		if isFK(err) {
			return model.Invalid("secret_id", "секрет не найден")
		}
		if err != nil {
			return err
		}
		return bumpDesired(ctx, tx)
	})
	if err != nil {
		return model.Source{}, err
	}
	return s.GetSource(ctx, id)
}

func (s *Store) UpdateSource(ctx context.Context, id string, rev int64, in model.SourceInput) (model.Source, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkRevision(ctx, tx, "sources", id, rev); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE sources SET name = ?, kind = ?, url = ?, interval_s = ?, timeout_s = ?, labels = ?, secret_id = ?, tls = ?, enabled = ?, revision = revision + 1 WHERE id = ?",
			in.Name, in.Kind, in.URL, in.IntervalS, in.TimeoutS, mustJSON(in.Labels), nullStr(in.SecretID), mustJSON(in.TLS), *in.Enabled, id)
		if isFK(err) {
			return model.Invalid("secret_id", "секрет не найден")
		}
		if err != nil {
			return err
		}
		return bumpDesired(ctx, tx)
	})
	if err != nil {
		return model.Source{}, err
	}
	return s.GetSource(ctx, id)
}

func (s *Store) DeleteSource(ctx context.Context, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if err := deleteByID(ctx, tx, "sources", id); err != nil {
			return err
		}
		return bumpDesired(ctx, tx)
	})
}

func (s *Store) SaveSourceStatus(ctx context.Context, id string, attempt, success time.Time, errText string) error {
	toNull := func(t time.Time) any {
		if t.IsZero() {
			return nil
		}
		return unix(t)
	}
	_, err := s.db.ExecContext(ctx, "UPDATE sources SET last_attempt = ?, last_success = coalesce(?, last_success), last_error = ? WHERE id = ?",
		toNull(attempt), toNull(success), errText, id)
	return err
}

type SecretRecord struct {
	model.Secret
	Payload    []byte
	KeyVersion int
}

func (s *Store) ListSecrets(ctx context.Context) ([]model.Secret, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.id, s.name, s.kind, s.mask, coalesce(json_group_array(src.id) FILTER (WHERE src.id IS NOT NULL), '[]')
		FROM secrets s LEFT JOIN sources src ON src.secret_id = s.id GROUP BY s.id ORDER BY s.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Secret{}
	for rows.Next() {
		var sec model.Secret
		var used string
		if err := rows.Scan(&sec.ID, &sec.Name, &sec.Kind, &sec.Mask, &used); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(used), &sec.UsedBy); err != nil {
			return nil, err
		}
		out = append(out, sec)
	}
	return out, rows.Err()
}

func (s *Store) GetSecret(ctx context.Context, id string) (SecretRecord, error) {
	var r SecretRecord
	err := s.db.QueryRowContext(ctx, "SELECT id, name, kind, mask, encrypted_payload, key_version FROM secrets WHERE id = ?", id).
		Scan(&r.ID, &r.Name, &r.Kind, &r.Mask, &r.Payload, &r.KeyVersion)
	return r, notFound(err)
}

func (s *Store) CreateSecret(ctx context.Context, id, name, kind, mask string, payload []byte, keyVersion int) (model.Secret, error) {
	_, err := s.db.ExecContext(ctx, "INSERT INTO secrets (id, name, kind, mask, encrypted_payload, key_version, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		id, name, kind, mask, payload, keyVersion, unix(s.now()))
	return model.Secret{ID: id, Name: name, Kind: kind, Mask: mask, UsedBy: []string{}}, err
}

func (s *Store) UpdateSecret(ctx context.Context, id, name, kind, mask string, payload []byte, keyVersion int) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "UPDATE secrets SET name = ?, kind = ?, mask = ?, encrypted_payload = ?, key_version = ? WHERE id = ?",
			name, kind, mask, payload, keyVersion, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return model.ErrNotFound
		}
		return bumpDesired(ctx, tx)
	})
}

func (s *Store) DeleteSecret(ctx context.Context, id string) error {
	return deleteByID(ctx, s.db, "secrets", id)
}

const presetCols = "id, title, expression, unit, legend, thresholds, min_step_s, revision"

func scanPreset(sc interface{ Scan(...any) error }) (model.Preset, error) {
	var p model.Preset
	var th string
	if err := sc.Scan(&p.ID, &p.Title, &p.Expression, &p.Unit, &p.Legend, &th, &p.MinStepS, &p.Revision); err != nil {
		return p, err
	}
	p.Category = "custom"
	p.Vars = model.PresetVarsOf(p.Expression)
	return p, json.Unmarshal([]byte(th), &p.Thresholds)
}

func (s *Store) ListPresets(ctx context.Context) ([]model.Preset, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+presetCols+" FROM presets ORDER BY title")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Preset{}
	for rows.Next() {
		p, err := scanPreset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetPreset(ctx context.Context, id string) (model.Preset, error) {
	p, err := scanPreset(s.db.QueryRowContext(ctx, "SELECT "+presetCols+" FROM presets WHERE id = ?", id))
	return p, notFound(err)
}

func (s *Store) CreatePreset(ctx context.Context, in model.PresetInput) (model.Preset, error) {
	id := model.NewID("qp")
	_, err := s.db.ExecContext(ctx, "INSERT INTO presets ("+presetCols+") VALUES (?, ?, ?, ?, ?, ?, ?, 1)",
		id, in.Title, in.Expression, in.Unit, in.Legend, mustJSON(in.Thresholds), in.MinStepS)
	if err != nil {
		return model.Preset{}, err
	}
	return s.GetPreset(ctx, id)
}

func (s *Store) UpdatePreset(ctx context.Context, id string, rev int64, in model.PresetInput) (model.Preset, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkRevision(ctx, tx, "presets", id, rev); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE presets SET title = ?, expression = ?, unit = ?, legend = ?, thresholds = ?, min_step_s = ?, revision = revision + 1 WHERE id = ?",
			in.Title, in.Expression, in.Unit, in.Legend, mustJSON(in.Thresholds), in.MinStepS, id)
		return err
	})
	if err != nil {
		return model.Preset{}, err
	}
	return s.GetPreset(ctx, id)
}

func (s *Store) DeletePreset(ctx context.Context, id string) error {
	return deleteByID(ctx, s.db, "presets", id)
}

func (s *Store) GetSettings(ctx context.Context) (model.Settings, error) {
	var st model.Settings
	var logo, start, public sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT title, logo_asset_id, start_page_id, public_page_id, revision FROM settings WHERE id = 1").
		Scan(&st.Title, &logo, &start, &public, &st.Revision)
	st.LogoAssetID, st.StartPageID, st.PublicPageID = strPtr(logo), strPtr(start), strPtr(public)
	st.RestartRequired = []string{}
	return st, err
}

func (s *Store) UpdateSettings(ctx context.Context, rev int64, in model.Settings) (model.Settings, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var cur int64
		if err := tx.QueryRowContext(ctx, "SELECT revision FROM settings WHERE id = 1").Scan(&cur); err != nil {
			return err
		}
		if cur != rev {
			return model.ErrConflict
		}
		for field, id := range map[string]*string{"start_page_id": in.StartPageID, "public_page_id": in.PublicPageID} {
			if id == nil {
				continue
			}
			var n int
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM pages WHERE id = ?", *id).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return model.Invalid(field, "страница не найдена")
			}
		}
		_, err := tx.ExecContext(ctx, "UPDATE settings SET title = ?, logo_asset_id = ?, start_page_id = ?, public_page_id = ?, revision = revision + 1 WHERE id = 1",
			in.Title, nullStr(in.LogoAssetID), nullStr(in.StartPageID), nullStr(in.PublicPageID))
		if isFK(err) {
			return model.Invalid("logo_asset_id", "файл не найден")
		}
		return err
	})
	if err != nil {
		return model.Settings{}, err
	}
	return s.GetSettings(ctx)
}

func (s *Store) ListAssets(ctx context.Context) ([]model.Asset, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, media_type, size, width, height, checksum, path, created_at FROM assets ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Asset{}
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func scanAsset(sc interface{ Scan(...any) error }) (model.Asset, error) {
	var a model.Asset
	var created int64
	err := sc.Scan(&a.ID, &a.MediaType, &a.Size, &a.Width, &a.Height, &a.Checksum, &a.Path, &created)
	a.CreatedAt = model.Time{Time: timeUnix(created).UTC()}
	a.URL = "/assets/" + a.ID
	return a, err
}

func (s *Store) GetAsset(ctx context.Context, id string) (model.Asset, error) {
	a, err := scanAsset(s.db.QueryRowContext(ctx, "SELECT id, media_type, size, width, height, checksum, path, created_at FROM assets WHERE id = ?", id))
	return a, notFound(err)
}

func (s *Store) AssetByChecksum(ctx context.Context, sum string) (model.Asset, error) {
	a, err := scanAsset(s.db.QueryRowContext(ctx, "SELECT id, media_type, size, width, height, checksum, path, created_at FROM assets WHERE checksum = ?", sum))
	return a, notFound(err)
}

func (s *Store) CreateAsset(ctx context.Context, a model.Asset) (model.Asset, error) {
	_, err := s.db.ExecContext(ctx, "INSERT INTO assets (id, media_type, size, width, height, checksum, path, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		a.ID, a.MediaType, a.Size, a.Width, a.Height, a.Checksum, a.Path, unix(s.now()))
	if err != nil {
		return a, err
	}
	return s.GetAsset(ctx, a.ID)
}

func (s *Store) AssetInUse(ctx context.Context, id string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM services WHERE icon = 'asset:' || ?1) +
		(SELECT count(*) FROM settings WHERE logo_asset_id = ?1) +
		(SELECT count(*) FROM pages WHERE json_extract(theme, '$.background_asset_id') = ?1)`, id).Scan(&n)
	return n > 0, err
}

func (s *Store) DeleteAsset(ctx context.Context, id string) error {
	return deleteByID(ctx, s.db, "assets", id)
}

type ConfigState struct {
	Desired int64
	Applied int64
	Error   string
}

func bumpDesired(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, "UPDATE config_state SET desired_revision = desired_revision + 1 WHERE id = 1")
	return err
}

func (s *Store) ConfigState(ctx context.Context) (ConfigState, error) {
	var c ConfigState
	err := s.db.QueryRowContext(ctx, "SELECT desired_revision, applied_revision, error FROM config_state WHERE id = 1").Scan(&c.Desired, &c.Applied, &c.Error)
	return c, err
}

func (s *Store) SetConfigApplied(ctx context.Context, applied int64, errText string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE config_state SET applied_revision = CASE WHEN ?1 > 0 THEN ?1 ELSE applied_revision END, error = ?2, updated_at = ?3 WHERE id = 1",
		applied, errText, unix(s.now()))
	return err
}

func (s *Store) AddRevision(ctx context.Context, reason, path string) (int64, error) {
	res, err := s.db.ExecContext(ctx, "INSERT INTO config_revisions (schema_version, reason, path, created_at) VALUES (?, ?, ?, ?)",
		SchemaVersion(), reason, path, unix(s.now()))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListRevisions(ctx context.Context) ([]model.ConfigRevision, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, schema_version, reason, created_at FROM config_revisions ORDER BY id DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ConfigRevision{}
	for rows.Next() {
		var r model.ConfigRevision
		var created int64
		if err := rows.Scan(&r.ID, &r.SchemaVersion, &r.Reason, &created); err != nil {
			return nil, err
		}
		r.CreatedAt = model.Time{Time: timeUnix(created).UTC()}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) DataVersion(ctx context.Context) (string, error) {
	return s.GetMeta(ctx, "change_counter")
}
