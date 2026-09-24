package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/fess932/homeLab/internal/model"
)

func (s *Store) ListPages(ctx context.Context) ([]model.PageSummary, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, title, slug, ord, revision FROM pages ORDER BY ord, title")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PageSummary{}
	for rows.Next() {
		var p model.PageSummary
		if err := rows.Scan(&p.ID, &p.Title, &p.Slug, &p.Order, &p.Revision); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetPage(ctx context.Context, idOrSlug string) (model.Page, error) {
	return getPage(ctx, s.db, idOrSlug)
}

func getPage(ctx context.Context, q queryer, idOrSlug string) (model.Page, error) {
	var p model.Page
	var theme string
	var updated int64
	err := q.QueryRowContext(ctx, "SELECT id, title, slug, ord, theme, revision, updated_at FROM pages WHERE id = ? OR slug = ?", idOrSlug, idOrSlug).
		Scan(&p.ID, &p.Title, &p.Slug, &p.Order, &theme, &p.Revision, &updated)
	if err != nil {
		return p, notFound(err)
	}
	p.UpdatedAt = *model.TimePtr(timeUnix(updated))
	p.Theme = model.DefaultTheme()
	if err := json.Unmarshal([]byte(theme), &p.Theme); err != nil {
		return p, err
	}
	groups, err := pageGroups(ctx, q, p.ID)
	if err != nil {
		return p, err
	}
	p.Groups = groups
	p.Widgets = []model.Widget{}
	rows, err := q.QueryContext(ctx, "SELECT id, group_id, type, config, layout FROM widgets WHERE page_id = ? ORDER BY ord", p.ID)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var w model.Widget
		var cfg, layout string
		if err := rows.Scan(&w.ID, &w.GroupID, &w.Type, &cfg, &layout); err != nil {
			return p, err
		}
		w.Config = json.RawMessage(cfg)
		if err := json.Unmarshal([]byte(layout), &w.Layout); err != nil {
			return p, err
		}
		p.Widgets = append(p.Widgets, w)
	}
	return p, rows.Err()
}

func (s *Store) CreatePage(ctx context.Context, in model.PageInput) (model.Page, error) {
	id := model.NewID("pg")
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if in.Order == 0 {
			if err := tx.QueryRowContext(ctx, "SELECT coalesce(max(ord), -1) + 1 FROM pages").Scan(&in.Order); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO pages (id, title, slug, ord, theme, revision, updated_at) VALUES (?, ?, ?, ?, ?, 1, ?)",
			id, in.Title, in.Slug, in.Order, mustJSON(in.Theme), unix(s.now()))
		if isUnique(err) {
			return model.Invalid("slug", "страница с таким адресом уже есть")
		}
		if err != nil {
			return err
		}
		if err := writePageContent(ctx, tx, id, in, func(string) bool { return false }); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE settings SET start_page_id = ?, revision = revision + 1 WHERE id = 1 AND start_page_id IS NULL", id)
		return err
	})
	if err != nil {
		return model.Page{}, err
	}
	return s.GetPage(ctx, id)
}

func (s *Store) UpdatePage(ctx context.Context, id string, rev int64, in model.PageInput) (model.Page, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkRevision(ctx, tx, "pages", id, rev); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE pages SET title = ?, slug = ?, ord = ?, theme = ?, revision = revision + 1, updated_at = ? WHERE id = ?",
			in.Title, in.Slug, in.Order, mustJSON(in.Theme), unix(s.now()), id)
		if isUnique(err) {
			return model.Invalid("slug", "страница с таким адресом уже есть")
		}
		if err != nil {
			return err
		}
		reuse, err := existingIDs(ctx, tx, id)
		if err != nil {
			return err
		}
		return writePageContent(ctx, tx, id, in, reuse)
	})
	if err != nil {
		return model.Page{}, err
	}
	return s.GetPage(ctx, id)
}

func existingIDs(ctx context.Context, tx *sql.Tx, pageID string) (func(string) bool, error) {
	existing, err := queryStrings(ctx, tx, "SELECT id FROM page_groups WHERE page_id = ?1 UNION ALL SELECT id FROM widgets WHERE page_id = ?1", pageID)
	if err != nil {
		return nil, err
	}
	keep := map[string]bool{}
	for _, id := range existing {
		keep[id] = true
	}
	return func(id string) bool { return keep[id] }, nil
}

func writePageContent(ctx context.Context, tx *sql.Tx, pageID string, in model.PageInput, reuse func(string) bool) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM widgets WHERE page_id = ?", pageID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM page_groups WHERE page_id = ?", pageID); err != nil {
		return err
	}
	ids := map[string]string{}
	resolve := func(id, prefix string) string {
		if v, ok := ids[id]; ok {
			return v
		}
		if strings.HasPrefix(id, prefix+"_") && reuse(id) {
			ids[id] = id
		} else {
			ids[id] = model.NewID(prefix)
		}
		return ids[id]
	}
	for i, g := range in.Groups {
		if _, err := tx.ExecContext(ctx, "INSERT INTO page_groups (id, page_id, title, collapsed, ord) VALUES (?, ?, ?, ?, ?)",
			resolve(g.ID, "grp"), pageID, g.Title, g.Collapsed, i); err != nil {
			return err
		}
	}
	for i, w := range in.Widgets {
		if _, err := tx.ExecContext(ctx, "INSERT INTO widgets (id, page_id, group_id, type, config, layout, ord) VALUES (?, ?, ?, ?, ?, ?, ?)",
			resolve(w.ID, "wgt"), pageID, resolve(w.GroupID, "grp"), w.Type, string(w.Config), mustJSON(w.Layout), i); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeletePage(ctx context.Context, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "DELETE FROM pages WHERE id = ?", id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return model.ErrNotFound
		}
		_, err = tx.ExecContext(ctx, `UPDATE settings SET
			start_page_id = CASE WHEN start_page_id = ?1 THEN (SELECT id FROM pages ORDER BY ord LIMIT 1) ELSE start_page_id END,
			public_page_id = CASE WHEN public_page_id = ?1 THEN NULL ELSE public_page_id END,
			revision = revision + 1 WHERE id = 1`, id)
		return err
	})
}

func (s *Store) Widget(ctx context.Context, id string) (model.Widget, string, error) {
	var w model.Widget
	var pageID, cfg, layout string
	err := s.db.QueryRowContext(ctx, "SELECT id, page_id, group_id, type, config, layout FROM widgets WHERE id = ?", id).
		Scan(&w.ID, &pageID, &w.GroupID, &w.Type, &cfg, &layout)
	if err != nil {
		return w, "", notFound(err)
	}
	w.Config = json.RawMessage(cfg)
	return w, pageID, json.Unmarshal([]byte(layout), &w.Layout)
}

func pageGroups(ctx context.Context, q queryer, pageID string) ([]model.Group, error) {
	rows, err := q.QueryContext(ctx, "SELECT id, title, collapsed FROM page_groups WHERE page_id = ? ORDER BY ord", pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Group{}
	for rows.Next() {
		var g model.Group
		if err := rows.Scan(&g.ID, &g.Title, &g.Collapsed); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
