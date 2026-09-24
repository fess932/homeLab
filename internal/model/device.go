package model

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	deviceKindRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	// Эти метки HomeDeck ставит сам на каждый ряд устройства.
	deviceReserved = []string{"device_id", "device", "key", "unit", "value", "source_id", "job", "instance", "service_id"}
)

// DeviceInput — общие настройки устройства. Всё, что относится к конкретному
// драйверу (протокол, id в облаке, описание точек данных), лежит в Config и
// проверяется самим драйвером (пакет drivers).
type DeviceInput struct {
	Name      string            `json:"name"`
	Kind      string            `json:"kind"`
	Address   string            `json:"address"`
	IntervalS int               `json:"interval_s"`
	TimeoutS  int               `json:"timeout_s"`
	Labels    map[string]string `json:"labels"`
	SecretID  *string           `json:"secret_id"`
	Enabled   *bool             `json:"enabled"`
	Config    json.RawMessage   `json:"config"`
}

// Reading — одно значение устройства после драйвера: общий ключ величины, единица
// HomeDeck и число. У перечислений дополнительно есть текстовое состояние.
type Reading struct {
	Key   string  `json:"key"`
	Unit  string  `json:"unit"`
	Value float64 `json:"value"`
	State string  `json:"state,omitempty"`
	// Thresholds — нормы значения: пороги тревоги с устройства или типовые нормы драйвера.
	Thresholds []Threshold `json:"thresholds,omitempty"`
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
	if len(d.Config) == 0 || string(d.Config) == "null" {
		d.Config = json.RawMessage("{}")
	}
}

// Validate проверяет общие поля; адрес и Config проверяет драйвер (drivers.Validate).
func (d *DeviceInput) Validate() error {
	var v validator
	v.check(d.Name != "" && utf8.RuneCountInString(d.Name) <= 100, "name", "от 1 до 100 символов")
	v.check(deviceKindRe.MatchString(d.Kind), "kind", "тип устройства")
	v.check(utf8.RuneCountInString(d.Address) <= 500, "address", "до 500 символов")
	v.check(d.IntervalS >= 5 && d.IntervalS <= 3600, "interval_s", "от 5 до 3600 секунд")
	v.check(d.TimeoutS >= 1 && d.TimeoutS < d.IntervalS, "timeout_s", "от 1 секунды и меньше интервала")
	v.check(len(d.Config) <= 64<<10 && json.Valid(d.Config), "config", "JSON до 64 КиБ")
	v.check(len(d.Labels) <= 20, "labels", "не более 20 меток")
	for k, val := range d.Labels {
		v.check(labelRe.MatchString(k) && !strings.HasPrefix(k, "__") && !oneOf(k, deviceReserved...), "labels."+k, "недопустимое имя метки")
		v.check(utf8.RuneCountInString(val) <= 200, "labels."+k, "значение до 200 символов")
	}
	return v.err()
}
