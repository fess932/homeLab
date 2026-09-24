package drivers

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/fess932/homeLab/internal/probe"
)

// Classify переводит ошибку опроса в вид и текст для UI.
func Classify(err error) (kind, msg string) {
	if e := (*Error)(nil); errors.As(err, &e) {
		return e.Kind, e.Msg
	}
	if errors.Is(err, io.EOF) {
		return KindProtocol, "устройство закрыло соединение: проверьте ключ, id устройства и версию протокола"
	}
	return probe.Classify(err)
}

// Number читает число из JSON: json.Number, float64 или строку с числом.
func Number(v any) (float64, bool) {
	var f float64
	var err error
	switch n := v.(type) {
	case json.Number:
		f, err = n.Float64()
	case float64:
		f = n
	case string:
		f, err = strconv.ParseFloat(strings.TrimSpace(n), 64)
	default:
		return 0, false
	}
	return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
}

func BoolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// MaxState — предел длины текстового состояния, чтобы метки рядов не разрастались.
const MaxState = 64

func Clip(s string) string {
	if utf8.RuneCountInString(s) <= MaxState {
		return s
	}
	return string([]rune(s)[:MaxState])
}

// CanonicalUnit переводит единицу устройства в единицу HomeDeck; неизвестная — пустая.
func CanonicalUnit(u string) string {
	switch strings.TrimSpace(u) {
	case "℃", "°C", "C":
		return "celsius"
	case "%":
		return "percent"
	case "ppm":
		return "ppm"
	case "ug/m³", "μg/m³", "µg/m³", "ug/m3":
		return "ugm3"
	case "mg/m³", "mg/m3":
		return "mgm3"
	}
	return ""
}

// Decode разбирает настройки драйвера строго: опечатка в имени поля — ошибка, а не молчание.
func Decode(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	return dec.Decode(v)
}
