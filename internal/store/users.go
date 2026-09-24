package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/fess932/homeLab/internal/model"
)

type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
}

type Session struct {
	UserID    string
	Username  string
	CSRFToken string
	ExpiresAt time.Time
}

func (s *Store) HasUsers(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM users").Scan(&n)
	return n > 0, err
}

func (s *Store) CreateFirstUser(ctx context.Context, u User, title string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM users").Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return model.ErrConflict
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO users (id, username, password_hash, created_at) VALUES (?, ?, ?, ?)",
			u.ID, u.Username, u.PasswordHash, unix(s.now())); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE settings SET title = ?, revision = revision + 1 WHERE id = 1", title)
		return err
	})
}

func (s *Store) UserByName(ctx context.Context, name string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, "SELECT id, username, password_hash FROM users WHERE username = ?", name).
		Scan(&u.ID, &u.Username, &u.PasswordHash)
	return u, notFound(err)
}

func (s *Store) UserByID(ctx context.Context, id string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, "SELECT id, username, password_hash FROM users WHERE id = ?", id).
		Scan(&u.ID, &u.Username, &u.PasswordHash)
	return u, notFound(err)
}

func (s *Store) SetPassword(ctx context.Context, userID, hash string, keepSession []byte) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE users SET password_hash = ? WHERE id = ?", hash, userID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ? AND token_hash != ?", userID, keepSession)
		return err
	})
}

func (s *Store) CreateSession(ctx context.Context, tokenHash []byte, userID, csrf string, ttl time.Duration) error {
	now := s.now()
	_, err := s.db.ExecContext(ctx, "INSERT INTO sessions (token_hash, user_id, csrf_token, created_at, expires_at) VALUES (?, ?, ?, ?, ?)",
		tokenHash, userID, csrf, unix(now), unix(now.Add(ttl)))
	return err
}

func (s *Store) SessionByHash(ctx context.Context, tokenHash []byte) (Session, error) {
	var ss Session
	var exp int64
	err := s.db.QueryRowContext(ctx, `SELECT s.user_id, u.username, s.csrf_token, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token_hash = ? AND s.expires_at > ?`,
		tokenHash, unix(s.now())).Scan(&ss.UserID, &ss.Username, &ss.CSRFToken, &exp)
	ss.ExpiresAt = time.Unix(exp, 0)
	return ss, notFound(err)
}

func (s *Store) ExtendSession(ctx context.Context, tokenHash []byte, ttl time.Duration) error {
	_, err := s.db.ExecContext(ctx, "UPDATE sessions SET expires_at = ? WHERE token_hash = ?", unix(s.now().Add(ttl)), tokenHash)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	return err
}

func (s *Store) PurgeSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", unix(s.now()))
	return err
}
