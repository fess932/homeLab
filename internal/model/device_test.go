package model

import (
	"errors"
	"testing"
)

func TestDeviceValidate(t *testing.T) {
	sec := "sec_x"
	tuya := func(mod func(*DeviceInput)) DeviceInput {
		d := DeviceInput{Name: "Датчик", Kind: DeviceTuya, Address: "192.168.0.235", SecretID: &sec, Tuya: &TuyaConfig{DeviceID: "eb398c7f26966400abs3ju"}}
		if mod != nil {
			mod(&d)
		}
		d.Normalize()
		return d
	}
	jsonDev := func(mod func(*DeviceInput)) DeviceInput {
		d := DeviceInput{Name: "Shelly", Kind: DeviceHTTPJSON, Address: "http://shelly.lan/status", HTTPJSON: &HTTPJSONConfig{Fields: []JSONField{{Path: "meters.0.power", Key: "power"}}}}
		if mod != nil {
			mod(&d)
		}
		d.Normalize()
		return d
	}
	ok := []DeviceInput{
		tuya(nil),
		tuya(func(d *DeviceInput) { d.Address = "sensor.lan:6668" }),
		// Выключенное устройство можно сохранить без ключа — так его возвращает импорт.
		tuya(func(d *DeviceInput) { d.SecretID, d.Enabled = nil, new(false) }),
		jsonDev(nil),
	}
	for i, d := range ok {
		if err := d.Validate(); err != nil {
			t.Errorf("ok[%d]: %v", i, err)
		}
	}
	if d := tuya(nil); d.Tuya.Version != "auto" || d.IntervalS != 30 || d.TimeoutS != 5 || d.HTTPJSON != nil {
		t.Errorf("значения по умолчанию: %+v", d)
	}
	if d := jsonDev(nil); d.HTTPJSON.Fields[0].Scale != 1 {
		t.Errorf("множитель по умолчанию: %+v", d.HTTPJSON.Fields[0])
	}
	bad := map[string]DeviceInput{
		"secret_id":      tuya(func(d *DeviceInput) { d.SecretID = nil }),
		"tuya.device_id": tuya(func(d *DeviceInput) { d.Tuya.DeviceID = "x" }),
		"tuya.version":   tuya(func(d *DeviceInput) { d.Tuya.Version = "3.1" }),
		"address":        tuya(func(d *DeviceInput) { d.Address = "" }),
		"tuya.schema[1].dp": tuya(func(d *DeviceInput) {
			d.Tuya.Schema = []TuyaDP{{DP: "2", Code: "a", Type: "Integer"}, {DP: "2", Code: "b", Type: "Integer"}}
		}),
		"labels.device":           tuya(func(d *DeviceInput) { d.Labels = map[string]string{"device": "x"} }),
		"kind":                    tuya(func(d *DeviceInput) { d.Kind = "zigbee" }),
		"http_json.fields[0].key": jsonDev(func(d *DeviceInput) { d.HTTPJSON.Fields[0].Key = "Power W" }),
		"http_json.fields[0].path": jsonDev(func(d *DeviceInput) {
			d.HTTPJSON.Fields[0].Path = "a..b"
		}),
		"http_json.fields": jsonDev(func(d *DeviceInput) { d.HTTPJSON.Fields = nil }),
	}
	for field, d := range bad {
		err := d.Validate()
		ve, isVE := errors.AsType[*ValidationError](err)
		if !isVE || ve.Fields[field] == "" {
			t.Errorf("%s: ожидалась ошибка поля, получено %v", field, err)
		}
	}
}
