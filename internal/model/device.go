package model

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	DeviceTuya     = "tuya"
	DeviceHTTPJSON = "http_json"

	TuyaDefaultPort = "6668"
)

var (
	TuyaVersions = []string{"auto", "3.3", "3.4", "3.5"}
	tuyaIDRe     = regexp.MustCompile(`^[a-zA-Z0-9]{10,32}$`)
	tuyaCodeRe   = regexp.MustCompile(`^[a-zA-Z0-9_]{1,64}$`)
	readingKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	jsonPathRe   = regexp.MustCompile(`^[^.\s]+(\.[^.\s]+)*$`)
	// Эти метки HomeDeck ставит сам на каждый ряд устройства.
	deviceReserved = []string{"device_id", "device", "key", "unit", "value", "source_id", "job", "instance", "service_id"}
)

// TuyaDP описывает одну точку данных устройства Tuya: номер, код и формат значения.
// Берётся из JSON устройства (status_range и local_strategy), который отдаёт облако Tuya.
type TuyaDP struct {
	DP    string   `json:"dp"`
	Code  string   `json:"code"`
	Type  string   `json:"type"`
	Unit  string   `json:"unit"`
	Scale int      `json:"scale"`
	Range []string `json:"range,omitempty"`
}

type TuyaConfig struct {
	DeviceID string   `json:"device_id"`
	Version  string   `json:"version"`
	Schema   []TuyaDP `json:"schema"`
}

// JSONField — значение из ответа HTTP JSON: путь через точку (массивы — номером),
// ключ метрики, единица и множитель.
type JSONField struct {
	Path  string  `json:"path"`
	Key   string  `json:"key"`
	Unit  string  `json:"unit"`
	Scale float64 `json:"scale"`
}

type HTTPJSONConfig struct {
	Fields []JSONField `json:"fields"`
}

type DeviceInput struct {
	Name      string            `json:"name"`
	Kind      string            `json:"kind"`
	Address   string            `json:"address"`
	IntervalS int               `json:"interval_s"`
	TimeoutS  int               `json:"timeout_s"`
	Labels    map[string]string `json:"labels"`
	SecretID  *string           `json:"secret_id"`
	Enabled   *bool             `json:"enabled"`
	Tuya      *TuyaConfig       `json:"tuya,omitempty"`
	HTTPJSON  *HTTPJSONConfig   `json:"http_json,omitempty"`
}

// Reading — одно значение устройства после драйвера: общий ключ величины, единица
// HomeDeck и число. У перечислений дополнительно есть текстовое состояние.
type Reading struct {
	Key   string  `json:"key"`
	Unit  string  `json:"unit"`
	Value float64 `json:"value"`
	State string  `json:"state,omitempty"`
}

type DeviceStatus struct {
	State       string    `json:"state"`
	LastAttempt *Time     `json:"last_attempt"`
	LastSuccess *Time     `json:"last_success"`
	DurationMS  *float64  `json:"duration_ms"`
	Error       string    `json:"error"`
	ErrorKind   string    `json:"error_kind"`
	Protocol    string    `json:"protocol,omitempty"`
	Readings    []Reading `json:"readings"`
}

type Device struct {
	ID string `json:"id"`
	DeviceInput
	Revision int64        `json:"revision"`
	Status   DeviceStatus `json:"status"`
}

func (d *DeviceInput) Normalize() {
	d.Name = strings.TrimSpace(d.Name)
	d.Address = strings.TrimSpace(d.Address)
	if d.IntervalS == 0 {
		d.IntervalS = 30
	}
	if d.TimeoutS == 0 {
		d.TimeoutS = min(5, d.IntervalS-1)
	}
	if d.Labels == nil {
		d.Labels = map[string]string{}
	}
	if d.SecretID != nil && *d.SecretID == "" {
		d.SecretID = nil
	}
	if d.Enabled == nil {
		d.Enabled = new(true)
	}
	switch d.Kind {
	case DeviceTuya:
		d.HTTPJSON = nil
		if d.Tuya == nil {
			d.Tuya = &TuyaConfig{}
		}
		d.Tuya.DeviceID = strings.TrimSpace(d.Tuya.DeviceID)
		if d.Tuya.Version == "" {
			d.Tuya.Version = "auto"
		}
		if d.Tuya.Schema == nil {
			d.Tuya.Schema = []TuyaDP{}
		}
	case DeviceHTTPJSON:
		d.Tuya = nil
		if d.HTTPJSON == nil {
			d.HTTPJSON = &HTTPJSONConfig{}
		}
		if d.HTTPJSON.Fields == nil {
			d.HTTPJSON.Fields = []JSONField{}
		}
		for i := range d.HTTPJSON.Fields {
			f := &d.HTTPJSON.Fields[i]
			f.Path, f.Key = strings.TrimSpace(f.Path), strings.TrimSpace(f.Key)
			if f.Scale == 0 {
				f.Scale = 1
			}
		}
	}
}

func (d *DeviceInput) Validate() error {
	var v validator
	v.check(d.Name != "" && utf8.RuneCountInString(d.Name) <= 100, "name", "от 1 до 100 символов")
	v.check(d.IntervalS >= 5 && d.IntervalS <= 3600, "interval_s", "от 5 до 3600 секунд")
	v.check(d.TimeoutS >= 1 && d.TimeoutS < d.IntervalS, "timeout_s", "от 1 секунды и меньше интервала")
	v.check(len(d.Labels) <= 20, "labels", "не более 20 меток")
	for k, val := range d.Labels {
		v.check(labelRe.MatchString(k) && !strings.HasPrefix(k, "__") && !oneOf(k, deviceReserved...), "labels."+k, "недопустимое имя метки")
		v.check(utf8.RuneCountInString(val) <= 200, "labels."+k, "значение до 200 символов")
	}
	switch d.Kind {
	case DeviceTuya:
		v.check(validHostPort(TuyaAddress(d.Address)), "address", "IP-адрес или имя устройства в локальной сети, порт по умолчанию 6668")
		// Выключенное устройство можно сохранить без ключа — так оно приходит из импорта.
		v.check(d.SecretID != nil || !*d.Enabled, "secret_id", "нужен секрет с local_key устройства")
		c := d.Tuya
		v.check(tuyaIDRe.MatchString(c.DeviceID), "tuya.device_id", "id устройства из приложения или JSON Tuya")
		v.check(oneOf(c.Version, TuyaVersions...), "tuya.version", "auto, 3.3, 3.4 или 3.5")
		v.check(len(c.Schema) <= 128, "tuya.schema", "не более 128 точек данных")
		seen := map[string]bool{}
		for i, dp := range c.Schema {
			f := fmt.Sprintf("tuya.schema[%d]", i)
			v.check(isDigits(dp.DP) && !seen[dp.DP], f+".dp", "номер точки данных без повторов")
			seen[dp.DP] = true
			v.check(tuyaCodeRe.MatchString(dp.Code), f+".code", "латиница, цифры и _")
			v.check(oneOf(dp.Type, "Integer", "Boolean", "Enum", "String", "Raw", "Bitmap", "Json"), f+".type", "тип точки данных Tuya")
			v.check(dp.Scale >= 0 && dp.Scale <= 9, f+".scale", "от 0 до 9")
			v.check(utf8.RuneCountInString(dp.Unit) <= 20 && len(dp.Range) <= 64, f, "слишком длинное описание")
		}
	case DeviceHTTPJSON:
		u, err := url.Parse(d.Address)
		v.check(err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.Fragment == "",
			"address", "адрес http:// или https:// без логина и пароля; credentials задаются секретом")
		fields := d.HTTPJSON.Fields
		v.check(len(fields) >= 1 && len(fields) <= 64, "http_json.fields", "от 1 до 64 полей")
		keys := map[string]bool{}
		for i, fl := range fields {
			f := fmt.Sprintf("http_json.fields[%d]", i)
			v.check(len(fl.Path) <= 256 && jsonPathRe.MatchString(fl.Path), f+".path", "путь через точку, например sensors.0.temp")
			v.check(readingKeyRe.MatchString(fl.Key) && !keys[fl.Key], f+".key", "латиница в нижнем регистре, цифры и _, без повторов")
			keys[fl.Key] = true
			v.check(oneOf(fl.Unit, Units...), f+".unit", "неизвестная единица")
		}
	default:
		v.add("kind", "tuya или http_json")
	}
	return v.err()
}

// TuyaAddress дополняет адрес устройства Tuya стандартным портом локального протокола.
func TuyaAddress(addr string) string {
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	return addr + ":" + TuyaDefaultPort
}

func isDigits(s string) bool {
	if s == "" || len(s) > 4 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
