package store

import (
	"context"
	"database/sql"
	"regexp"

	"github.com/fess932/homeLab/internal/model"
)

var idRe = regexp.MustCompile(`^[a-z]+_[a-z0-9]{14}$`)

type Snapshot struct {
	Settings model.Settings
	Pages    []model.Page
	Services []model.Service
	Checks   []model.Check
	Sources  []model.Source
	Devices  []model.Device
	Presets  []model.Preset
}

type ReplaceScope struct {
	Pages    bool
	Services bool
	Sources  bool
	Devices  bool
	Presets  bool
}

func (s *Store) Snapshot(ctx context.Context) (Snapshot, error) {
	var snap Snapshot
	var err error
	if snap.Settings, err = s.GetSettings(ctx); err != nil {
		return snap, err
	}
	summaries, err := s.ListPages(ctx)
	if err != nil {
		return snap, err
	}
	for _, ps := range summaries {
		p, err := s.GetPage(ctx, ps.ID)
		if err != nil {
			return snap, err
		}
		snap.Pages = append(snap.Pages, p)
	}
	if snap.Services, err = s.ListServices(ctx); err != nil {
		return snap, err
	}
	if snap.Checks, err = s.ListChecks(ctx); err != nil {
		return snap, err
	}
	if snap.Sources, err = s.ListSources(ctx); err != nil {
		return snap, err
	}
	if snap.Devices, err = s.ListDevices(ctx); err != nil {
		return snap, err
	}
	snap.Presets, err = s.ListPresets(ctx)
	return snap, err
}

func (s *Store) Replace(ctx context.Context, scope ReplaceScope, snap Snapshot, expectedVersion string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var v string
		if err := tx.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = 'change_counter'").Scan(&v); err != nil {
			return err
		}
		if v != expectedVersion {
			return model.ErrConflict
		}
		now := unix(s.now())
		if scope.Pages {
			if _, err := tx.ExecContext(ctx, "DELETE FROM pages"); err != nil {
				return err
			}
		}
		if scope.Services {
			if _, err := tx.ExecContext(ctx, "DELETE FROM services"); err != nil {
				return err
			}
		}
		if scope.Presets {
			if _, err := tx.ExecContext(ctx, "DELETE FROM presets"); err != nil {
				return err
			}
		}
		if scope.Sources {
			if _, err := tx.ExecContext(ctx, "UPDATE services SET source_id = NULL"); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM sources"); err != nil {
				return err
			}
			for _, src := range snap.Sources {
				if _, err := tx.ExecContext(ctx, "INSERT INTO sources (id, name, kind, url, interval_s, timeout_s, labels, secret_id, tls, enabled, revision, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)",
					src.ID, src.Name, src.Kind, src.URL, src.IntervalS, src.TimeoutS, mustJSON(src.Labels), nullStr(src.SecretID), mustJSON(src.TLS), *src.Enabled, now); err != nil {
					return err
				}
			}
			if err := bumpDesired(ctx, tx); err != nil {
				return err
			}
		}
		if scope.Devices {
			if _, err := tx.ExecContext(ctx, "DELETE FROM devices"); err != nil {
				return err
			}
			for _, d := range snap.Devices {
				if _, err := tx.ExecContext(ctx, "INSERT INTO devices ("+deviceCols+", created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)",
					d.ID, d.Name, d.Kind, d.Address, d.IntervalS, d.TimeoutS, mustJSON(d.Labels), nullStr(d.SecretID), deviceConfigJSON(d.DeviceInput), *d.Enabled, now); err != nil {
					return err
				}
			}
		}
		if scope.Services {
			for _, sv := range snap.Services {
				if _, err := tx.ExecContext(ctx, "INSERT INTO services (id, name, description, url, icon, tags, open_mode, source_id, revision, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?)",
					sv.ID, sv.Name, sv.Description, sv.URL, sv.Icon, mustJSON(sv.Tags), sv.OpenMode, nullStr(sv.SourceID), now); err != nil {
					return err
				}
			}
			for _, c := range snap.Checks {
				if _, err := tx.ExecContext(ctx, "INSERT INTO checks ("+checkCols+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1)",
					c.ID, c.ServiceID, c.Kind, c.Target, c.ExpectedStatus, c.IntervalS, c.TimeoutS, *c.Enabled, c.CAPEM); err != nil {
					return err
				}
			}
		}
		if scope.Presets {
			for _, p := range snap.Presets {
				if _, err := tx.ExecContext(ctx, "INSERT INTO presets ("+presetCols+") VALUES (?, ?, ?, ?, ?, ?, ?, 1)",
					p.ID, p.Title, p.Expression, p.Unit, p.Legend, mustJSON(p.Thresholds), p.MinStepS); err != nil {
					return err
				}
			}
		}
		if scope.Pages {
			used := map[string]bool{}
			reuse := func(id string) bool {
				if used[id] || !idRe.MatchString(id) {
					return false
				}
				used[id] = true
				return true
			}
			for _, p := range snap.Pages {
				if _, err := tx.ExecContext(ctx, "INSERT INTO pages (id, title, slug, ord, theme, revision, updated_at) VALUES (?, ?, ?, ?, ?, 1, ?)",
					p.ID, p.Title, p.Slug, p.Order, mustJSON(p.Theme), now); err != nil {
					return err
				}
				if err := writePageContent(ctx, tx, p.ID, p.PageInput, reuse); err != nil {
					return err
				}
			}
			st := snap.Settings
			if _, err := tx.ExecContext(ctx, "UPDATE settings SET title = ?, start_page_id = ?, public_page_id = ?, revision = revision + 1 WHERE id = 1",
				st.Title, nullStr(st.StartPageID), nullStr(st.PublicPageID)); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE settings SET logo_asset_id = (SELECT id FROM assets WHERE id = ?) WHERE id = 1",
				nullStr(st.LogoAssetID)); err != nil {
				return err
			}
		}
		return nil
	})
}
