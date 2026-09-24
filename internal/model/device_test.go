package model

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestDeviceValidate(t *testing.T) {
	dev := func(mod func(*DeviceInput)) DeviceInput {
		d := DeviceInput{Name: "Датчик", Kind: "tuya", Address: "192.168.0.235", Config: json.RawMessage(`{"device_id":"x"}`)}
		if mod != nil {
			mod(&d)
		}
		d.Normalize()
		return d
	}
	good := dev(nil)
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	if d := dev(func(d *DeviceInput) { d.Config = nil }); string(d.Config) != "{}" || d.IntervalS != 30 || d.TimeoutS != 5 || !*d.Enabled {
		t.Errorf("значения по умолчанию: %+v", d)
	}
	bad := map[string]DeviceInput{
		"name":          dev(func(d *DeviceInput) { d.Name = " " }),
		"kind":          dev(func(d *DeviceInput) { d.Kind = "Zigbee!" }),
		"interval_s":    dev(func(d *DeviceInput) { d.IntervalS = 2 }),
		"timeout_s":     dev(func(d *DeviceInput) { d.TimeoutS = 30 }),
		"config":        dev(func(d *DeviceInput) { d.Config = json.RawMessage(`{`) }),
		"labels.device": dev(func(d *DeviceInput) { d.Labels = map[string]string{"device": "x"} }),
	}
	for field, d := range bad {
		err := d.Validate()
		ve, ok := errors.AsType[*ValidationError](err)
		if !ok || ve.Fields[field] == "" {
			t.Errorf("%s: ожидалась ошибка поля, получено %v", field, err)
		}
	}
}
