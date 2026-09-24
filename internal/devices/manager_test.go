package devices

import (
	"context"
	"strings"
	"testing"

	_ "github.com/fess932/homeLab/drivers/httpjson"
	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/secrets"
)

func TestWritePrometheus(t *testing.T) {
	m := &Manager{}
	m.Start()
	defer m.Stop()
	up := model.Device{ID: "dev_a", Name: `Датчик "спальня"`, Enabled: new(true), Labels: map[string]string{"room": "bed"}}
	down := model.Device{ID: "dev_b", Name: "Розетка", Enabled: new(true)}
	off := model.Device{ID: "dev_c", Name: "Выкл", Enabled: new(false)}
	pending := model.Device{ID: "dev_d", Name: "Новый", Enabled: new(true)}
	m.trackers = map[string]*tracker{
		"dev_a": {dev: up, status: model.DeviceStatus{State: model.StateUp, DurationMS: new(250.0), Readings: []model.Reading{
			{Key: "temperature", Unit: "celsius", Value: 23.5},
			{Key: "air_quality", Value: 1, State: "level_1"},
			{Key: "dp_112", State: "c"},
		}}},
		"dev_b": {dev: down, status: model.DeviceStatus{State: model.StateDown, DurationMS: new(5000.0), Readings: []model.Reading{{Key: "power", Value: 5}}}},
		"dev_c": {dev: off, status: model.DeviceStatus{State: model.StateDisabled}},
		"dev_d": {dev: pending, status: model.DeviceStatus{State: model.StatePending}},
	}
	var b strings.Builder
	m.WritePrometheus(&b)
	got := b.String()
	a := `device_id="dev_a",device="Датчик \"спальня\"",room="bed"`
	for _, want := range []string{
		"homedeck_device_up{" + a + "} 1\n",
		"homedeck_device_poll_duration_seconds{" + a + "} 0.25\n",
		"homedeck_device_value{" + a + `,key="temperature",unit="celsius"} 23.5` + "\n",
		"homedeck_device_state{" + a + `,key="air_quality",value="level_1"} 1` + "\n",
		"homedeck_device_value{" + a + `,key="air_quality",unit=""} 1` + "\n",
		"homedeck_device_state{" + a + `,key="dp_112",value="c"} 1` + "\n",
		`homedeck_device_up{device_id="dev_b",device="Розетка"} 0` + "\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("нет строки %q в\n%s", want, got)
		}
	}
	// Текстовое значение без числа не должно давать ряд value, а недоступное устройство — старых значений.
	for _, bad := range []string{`key="dp_112",unit=`, `key="power"`, "dev_c", "dev_d"} {
		if strings.Contains(got, bad) {
			t.Errorf("лишнее %q в\n%s", bad, got)
		}
	}
}

func TestManagerSyncAndTest(t *testing.T) {
	m := &Manager{Secret: func(context.Context, string) (secrets.Payload, error) { return secrets.Payload{}, nil }}
	m.Start()
	defer m.Stop()
	d := model.Device{ID: "dev_a", Revision: 1, Name: "x", Kind: "http_json", Address: "http://127.0.0.1:1/", IntervalS: 3600, TimeoutS: 1, Enabled: new(false),
		Config: []byte(`{"fields":[{"path":"a","key":"a","unit":"","scale":1}]}`)}
	m.Sync([]model.Device{d})
	if st := m.Status("dev_a"); st.State != model.StateDisabled {
		t.Fatalf("выключенное устройство: %+v", st)
	}
	m.Sync(nil)
	if st := m.Status("dev_a"); st.State != model.StatePending {
		t.Fatalf("удалённое устройство должно пропасть: %+v", st)
	}
	// Loopback закрыт политикой исходящих соединений — проверка должна это объяснить, а не зависнуть.
	st := m.Test(context.Background(), d.DeviceInput, nil)
	if st.State != model.StateDown || st.ErrorKind != "forbidden_address" {
		t.Fatalf("проверка loopback: %+v", st)
	}
}
