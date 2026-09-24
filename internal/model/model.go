package model

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("conflict")
	ErrForbidden = errors.New("forbidden")
	ErrInUse     = errors.New("in use")
)

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	keys := slices.Sorted(maps.Keys(e.Fields))
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+": "+e.Fields[k])
	}
	return "validation: " + strings.Join(parts, "; ")
}

type validator struct {
	fields map[string]string
}

func (v *validator) check(ok bool, field, msg string) bool {
	if !ok {
		v.add(field, msg)
	}
	return ok
}

func (v *validator) add(field, msg string) {
	if v.fields == nil {
		v.fields = map[string]string{}
	}
	if _, dup := v.fields[field]; !dup {
		v.fields[field] = msg
	}
}

func (v *validator) err() error {
	if len(v.fields) == 0 {
		return nil
	}
	return &ValidationError{Fields: v.fields}
}

func Invalid(field, msg string) error {
	return &ValidationError{Fields: map[string]string{field: msg}}
}

const idAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

func NewID(prefix string) string {
	b := make([]byte, 14)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = idAlphabet[int(b[i])%len(idAlphabet)]
	}
	return prefix + "_" + string(b)
}

type Time struct{ time.Time }

func (t Time) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.UTC().Format(time.RFC3339))
}

func (t *Time) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	p, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return fmt.Errorf("время %q: ожидается RFC 3339", s)
	}
	t.Time = p
	return nil
}

func TimePtr(t time.Time) *Time {
	if t.IsZero() {
		return nil
	}
	return &Time{t.UTC().Truncate(time.Millisecond)}
}
