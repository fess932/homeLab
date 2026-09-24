package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/fess932/homeLab/internal/auth"
	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/store"
)

type sessionView struct {
	User      userView `json:"user"`
	CSRFToken string   `json:"csrf_token"`
}

type userView struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

func (s *Server) getSetup(w http.ResponseWriter, r *http.Request) error {
	has, err := s.Store.HasUsers(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]bool{"required": !has})
	return nil
}

const (
	minPassword = 8
	maxPassword = 256
)

var passwordRule = fmt.Sprintf("от %d до %d символов", minPassword, maxPassword)

func validPassword(p string) error {
	if utf8.RuneCountInString(p) < minPassword || len(p) > maxPassword {
		return model.Invalid("password", passwordRule)
	}
	return nil
}

func (s *Server) postSetup(w http.ResponseWriter, r *http.Request) error {
	if !s.setupLimiter.Allow(s.clientIP(r)) {
		return &Error{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: "слишком много попыток, подождите минуту"}
	}
	var in struct {
		Token    string `json:"token"`
		Username string `json:"username"`
		Password string `json:"password"`
		Title    string `json:"title"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	expected, pending := s.SetupToken()
	if !pending {
		return &Error{Status: http.StatusConflict, Code: "conflict", Message: "первичная настройка уже выполнена"}
	}
	if !auth.EqualTokens(strings.TrimSpace(in.Token), expected) {
		return &Error{Status: http.StatusForbidden, Code: "forbidden", Message: "неверный setup-token"}
	}
	in.Username = strings.TrimSpace(in.Username)
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		in.Title = "HomeDeck"
	}
	fields := map[string]string{}
	if in.Username == "" || utf8.RuneCountInString(in.Username) > 64 {
		fields["username"] = "от 1 до 64 символов"
	}
	if err := validPassword(in.Password); err != nil {
		fields["password"] = passwordRule
	}
	if utf8.RuneCountInString(in.Title) > 100 {
		fields["title"] = "до 100 символов"
	}
	if len(fields) > 0 {
		return &model.ValidationError{Fields: fields}
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return err
	}
	u := store.User{ID: model.NewID("usr"), Username: in.Username, PasswordHash: hash}
	if err := s.Store.CreateFirstUser(r.Context(), u, in.Title); err != nil {
		if errors.Is(err, model.ErrConflict) {
			return &Error{Status: http.StatusConflict, Code: "conflict", Message: "первичная настройка уже выполнена"}
		}
		return err
	}
	s.SetupDone()
	s.Log.Info("administrator created", "username", u.Username)
	return s.startSession(w, r, u)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u store.User) error {
	token, csrf := auth.RandomToken(), auth.RandomToken()
	if err := s.Store.CreateSession(r.Context(), auth.HashToken(token), u.ID, csrf, sessionTTL); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: s.isHTTPS(r), MaxAge: int(sessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, sessionView{User: userView{u.ID, u.Username}, CSRFToken: csrf})
	return nil
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	if !s.loginLimiter.Allow(s.clientIP(r)) {
		return &Error{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: "слишком много попыток входа, подождите минуту"}
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if len(in.Password) > 256 {
		return &Error{Status: http.StatusUnauthorized, Code: "unauthorized", Message: "неверное имя пользователя или пароль"}
	}
	u, err := s.Store.UserByName(r.Context(), strings.TrimSpace(in.Username))
	if errors.Is(err, model.ErrNotFound) {
		auth.BurnPasswordCheck(in.Password)
		return &Error{Status: http.StatusUnauthorized, Code: "unauthorized", Message: "неверное имя пользователя или пароль"}
	}
	if err != nil {
		return err
	}
	ok, err := auth.VerifyPassword(u.PasswordHash, in.Password)
	if err != nil {
		return err
	}
	if !ok {
		s.Log.Warn("login failed", "username", u.Username, "ip", s.clientIP(r))
		return &Error{Status: http.StatusUnauthorized, Code: "unauthorized", Message: "неверное имя пользователя или пароль"}
	}
	return s.startSession(w, r, u)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) error {
	if sess := currentSession(r); sess != nil {
		if !auth.EqualTokens(r.Header.Get("X-CSRF-Token"), sess.CSRFToken) {
			return &Error{Status: http.StatusForbidden, Code: "csrf", Message: "неверный CSRF-токен"}
		}
		if err := s.Store.DeleteSession(r.Context(), sess.tokenHash); err != nil {
			return err
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) error {
	sess := currentSession(r)
	writeJSON(w, http.StatusOK, sessionView{User: userView{sess.UserID, sess.Username}, CSRFToken: sess.CSRFToken})
	return nil
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) error {
	if !s.loginLimiter.Allow(s.clientIP(r)) {
		return &Error{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: "слишком много попыток, подождите минуту"}
	}
	var in struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	sess := currentSession(r)
	u, err := s.Store.UserByID(r.Context(), sess.UserID)
	if err != nil {
		return err
	}
	ok, err := auth.VerifyPassword(u.PasswordHash, in.Current)
	if err != nil {
		return err
	}
	if !ok {
		return model.Invalid("current", "неверный текущий пароль")
	}
	if err := validPassword(in.Next); err != nil {
		return model.Invalid("next", "от 10 до 256 символов")
	}
	hash, err := auth.HashPassword(in.Next)
	if err != nil {
		return err
	}
	if err := s.Store.SetPassword(r.Context(), u.ID, hash, sess.tokenHash); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
