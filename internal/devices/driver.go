// Package devices опрашивает устройства с собственными API (Tuya, HTTP JSON) и
// приводит их значения к общему виду: ключ величины, единица HomeDeck и число.
// Дальше значения уходят в VictoriaMetrics через внутренний /metrics.
package devices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/netguard"
	"github.com/fess932/homeLab/internal/probe"
	"github.com/fess932/homeLab/internal/secrets"
)

const (
	KindAuth     = "auth"
	KindProtocol = "protocol"
	KindParse    = "parse"

	maxJSONBody = 1 << 20
	maxState    = 64
)

type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// protoError — устройство ответило, но не так, как ожидалось: неверный ключ, версия, формат.
type protoError struct{ kind, msg string }

func (e *protoError) Error() string { return e.msg }

// dialError — до устройства не удалось даже подключиться.
type dialError struct{ err error }

func (e *dialError) Error() string { return e.err.Error() }
func (e *dialError) Unwrap() error { return e.err }

func classify(err error) (kind, msg string) {
	if pe := (*protoError)(nil); errors.As(err, &pe) {
		return pe.kind, pe.msg
	}
	if errors.Is(err, io.EOF) {
		return KindProtocol, "устройство закрыло соединение: проверьте local_key, id устройства и версию протокола"
	}
	return probe.Classify(err)
}

// poll опрашивает устройство один раз. version — версия Tuya, определённая при прошлом опросе.
func poll(ctx context.Context, d model.DeviceInput, secret secrets.Payload, version, userAgent string) ([]model.Reading, string, error) {
	switch d.Kind {
	case model.DeviceTuya:
		if version == "" {
			version = d.Tuya.Version
		}
		dps, used, err := tuyaQuery(ctx, netguard.Dialer(0).DialContext, model.TuyaAddress(d.Address), d.Tuya.DeviceID, []byte(secret.Key), version)
		if err != nil {
			return nil, "", err
		}
		return tuyaReadings(dps, d.Tuya.Schema), used, nil
	case model.DeviceHTTPJSON:
		r, err := httpJSON(ctx, d, secret, userAgent)
		return r, "", err
	}
	return nil, "", fmt.Errorf("неизвестный тип устройства %q", d.Kind)
}

// Общие ключи величин: одинаковые для любых устройств, чтобы один запрос
// «температура» работал для датчиков разных производителей.
var canonicalKeys = map[string]string{
	"temp_current":       "temperature",
	"temp_value":         "temperature",
	"va_temperature":     "temperature",
	"temperature":        "temperature",
	"humidity_value":     "humidity",
	"va_humidity":        "humidity",
	"humidity":           "humidity",
	"co2_value":          "co2",
	"co2":                "co2",
	"pm25_value":         "pm25",
	"pm25":               "pm25",
	"pm1_value":          "pm1",
	"pm1":                "pm1",
	"pm10_value":         "pm10",
	"pm10":               "pm10",
	"ch2o_value":         "formaldehyde",
	"voc_value":          "voc",
	"battery_percentage": "battery",
	"battery_value":      "battery",
	"charge_state":       "charging",
	"air_quality_index":  "air_quality",
}

func canonicalUnit(u string) string {
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

// tuyaReadings переводит точки данных Tuya в значения HomeDeck. Точки, описанные в схеме
// устройства, получают общий ключ, единицу и масштаб; остальные отдаются как есть под
// ключом dp_<номер>, чтобы по ним можно было понять, что они значат.
func tuyaReadings(dps map[string]any, schema []model.TuyaDP) []model.Reading {
	byDP := map[string]model.TuyaDP{}
	for _, s := range schema {
		byDP[s.DP] = s
	}
	out := []model.Reading{}
	for dp, raw := range dps {
		s, known := byDP[dp]
		if !known {
			if r, ok := rawReading("dp_"+dp, raw); ok {
				out = append(out, r)
			}
			continue
		}
		key := canonicalKeys[s.Code]
		if key == "" {
			key = strings.ToLower(s.Code)
		}
		switch s.Type {
		case "Integer":
			if v, ok := number(raw); ok {
				out = append(out, model.Reading{Key: key, Unit: canonicalUnit(s.Unit), Value: v / math.Pow10(s.Scale)})
			}
		case "Boolean":
			if b, ok := raw.(bool); ok {
				out = append(out, model.Reading{Key: key, Unit: "bool", Value: boolValue(b)})
			}
		case "Enum":
			if st, ok := raw.(string); ok {
				// Числом служит позиция в перечислении с единицы: level_1 → 1.
				out = append(out, model.Reading{Key: key, Value: float64(slices.Index(s.Range, st) + 1), State: clip(st)})
			}
		}
	}
	slices.SortFunc(out, func(a, b model.Reading) int { return strings.Compare(a.Key, b.Key) })
	return out
}

func rawReading(key string, raw any) (model.Reading, bool) {
	switch v := raw.(type) {
	case bool:
		return model.Reading{Key: key, Unit: "bool", Value: boolValue(v)}, true
	case string:
		return model.Reading{Key: key, State: clip(v)}, v != ""
	}
	if f, ok := number(raw); ok {
		return model.Reading{Key: key, Value: f}, true
	}
	return model.Reading{}, false
}

func httpJSON(ctx context.Context, d model.DeviceInput, secret secrets.Payload, userAgent string) ([]model.Reading, error) {
	tr := netguard.Transport(nil)
	tr.DisableKeepAlives = true
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, CheckRedirect: netguard.NoRedirects}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.Address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	switch {
	case secret.Token != "":
		req.Header.Set("Authorization", "Bearer "+secret.Token)
	case secret.Username != "":
		req.SetBasicAuth(secret.Username, secret.Password)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &protoError{kind: probe.KindHTTPStatus, msg: fmt.Sprintf("устройство ответило HTTP %d", resp.StatusCode)}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxJSONBody {
		return nil, &protoError{kind: KindParse, msg: "ответ больше 1 МиБ"}
	}
	var doc any
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, &protoError{kind: KindParse, msg: "ответ не JSON: " + err.Error()}
	}
	out := []model.Reading{}
	var missing []string
	for _, f := range d.HTTPJSON.Fields {
		v, ok := lookup(doc, f.Path)
		if !ok {
			missing = append(missing, f.Path)
			continue
		}
		if b, isBool := v.(bool); isBool {
			out = append(out, model.Reading{Key: f.Key, Unit: f.Unit, Value: boolValue(b)})
			continue
		}
		n, ok := number(v)
		if !ok {
			missing = append(missing, f.Path)
			continue
		}
		out = append(out, model.Reading{Key: f.Key, Unit: f.Unit, Value: n * f.Scale})
	}
	if len(out) == 0 {
		return nil, &protoError{kind: KindParse, msg: "в ответе нет ни одного числового поля из настроек: " + strings.Join(missing, ", ")}
	}
	return out, nil
}

// lookup идёт по пути через точку: имена полей объектов и номера элементов массивов.
func lookup(doc any, path string) (any, bool) {
	cur := doc
	for part := range strings.SplitSeq(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[part]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(node) {
				return nil, false
			}
			cur = node[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

func number(v any) (float64, bool) {
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

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxState {
		return s
	}
	return string([]rune(s)[:maxState])
}
