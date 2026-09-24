package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/fess932/homeLab/internal/model"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Store struct {
	db  *sql.DB
	now func() time.Time
}

type CorruptError struct{ Detail string }

func (e *CorruptError) Error() string {
	return "SQLite повреждена: " + e.Detail + "; восстановите /data из резервной копии"
}

func Open(ctx context.Context, path string) (*Store, error) {
	q := url.Values{}
	for _, p := range []string{"foreign_keys(1)", "journal_mode(WAL)", "busy_timeout(10000)", "synchronous(NORMAL)"} {
		q.Add("_pragma", p)
	}
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)
	s := &Store{db: db, now: time.Now}
	if err := s.checkIntegrity(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.migrate(ctx, path); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) checkIntegrity(ctx context.Context) error {
	var res string
	if err := s.db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&res); err != nil {
		if se, ok := errors.AsType[*sqlite.Error](err); ok && (se.Code() == sqlite3.SQLITE_NOTADB || se.Code() == sqlite3.SQLITE_CORRUPT) {
			return &CorruptError{Detail: se.Error()}
		}
		return fmt.Errorf("открытие SQLite: %w", err)
	}
	if res != "ok" {
		return &CorruptError{Detail: res}
	}
	return nil
}

func Migrations() ([]string, error) {
	names, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	return names, nil
}

func SchemaVersion() int {
	names, _ := Migrations()
	return len(names)
}

func (s *Store) migrate(ctx context.Context, path string) error {
	names, err := Migrations()
	if err != nil {
		return err
	}
	var current int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	if current > len(names) {
		return fmt.Errorf("схема базы версии %d новее поддерживаемой %d: запустите более новый образ или восстановите резервную копию", current, len(names))
	}
	if current == len(names) {
		return nil
	}
	if current > 0 {
		backup := fmt.Sprintf("%s.pre-migration-%d", path, current)
		_ = os.Remove(backup)
		if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", backup); err != nil {
			return fmt.Errorf("копия перед миграцией: %w", err)
		}
	}
	for i := current; i < len(names); i++ {
		body, err := migrationsFS.ReadFile(names[i])
		if err != nil {
			return err
		}
		err = s.tx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, string(body)); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(i+1))
			return err
		})
		if err != nil {
			return fmt.Errorf("миграция %s: %w", names[i], err)
		}
	}
	return nil
}

func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func isUnique(err error) bool {
	se, ok := errors.AsType[*sqlite.Error](err)
	return ok && (se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE || se.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY)
}

func isFK(err error) bool {
	se, ok := errors.AsType[*sqlite.Error](err)
	return ok && (se.Code() == sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY || se.Code() == sqlite3.SQLITE_CONSTRAINT_TRIGGER && strings.Contains(se.Error(), "FOREIGN KEY"))
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return model.ErrNotFound
	}
	return err
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func unix(t time.Time) int64 { return t.Unix() }

func fromUnix(v sql.NullInt64) *model.Time {
	if !v.Valid {
		return nil
	}
	return model.TimePtr(time.Unix(v.Int64, 0))
}

func nullStr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func strPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func checkRevision(ctx context.Context, q queryer, table, id string, expected int64) error {
	var rev int64
	if err := q.QueryRowContext(ctx, "SELECT revision FROM "+table+" WHERE id = ?", id).Scan(&rev); err != nil {
		return notFound(err)
	}
	if rev != expected {
		return model.ErrConflict
	}
	return nil
}

func (s *Store) GetMeta(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = ?", key).Scan(&v)
	return v, notFound(err)
}

func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, value)
	return err
}

func queryStrings(ctx context.Context, q queryer, query string, args ...any) ([]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
