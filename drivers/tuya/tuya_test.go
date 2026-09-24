package tuya

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fess932/homeLab/internal/model"
)

func TestNormalize(t *testing.T) {
	d := &Driver{}
	addr, cfg, err := d.Normalize("192.168.0.235", json.RawMessage(`{"device_id":" eb398c7f26966400abs3ju "}`))
	if err != nil || addr != "192.168.0.235" || !strings.Contains(string(cfg), `"device_id":"eb398c7f26966400abs3ju"`) || !strings.Contains(string(cfg), `"version":"auto"`) {
		t.Fatalf("%s %s %v", addr, cfg, err)
	}
	if withPort("192.168.0.235") != "192.168.0.235:6668" || withPort("sensor.lan:7000") != "sensor.lan:7000" {
		t.Fatal("порт по умолчанию")
	}
	for field, c := range map[string]struct{ addr, cfg string }{
		"address":               {"", `{"device_id":"eb398c7f26966400abs3ju"}`},
		"config.device_id":      {"x", `{"device_id":"x"}`},
		"config.version":        {"x", `{"device_id":"eb398c7f26966400abs3ju","version":"3.1"}`},
		"config.schema[1].dp":   {"x", `{"device_id":"eb398c7f26966400abs3ju","schema":[{"dp":"2","code":"a","type":"Integer"},{"dp":"2","code":"b","type":"Integer"}]}`},
		"config.schema[0].type": {"x", `{"device_id":"eb398c7f26966400abs3ju","schema":[{"dp":"2","code":"a","type":"Float"}]}`},
		"config":                {"x", `{"deviceid":"eb398c7f26966400abs3ju"}`},
	} {
		_, _, err := d.Normalize(c.addr, json.RawMessage(c.cfg))
		ve, ok := err.(*model.ValidationError)
		if !ok || ve.Fields[field] == "" {
			t.Errorf("%s: %v", field, err)
		}
	}
	if id := d.Identity(json.RawMessage(`{"device_id":"abc"}`)); id != "abc" {
		t.Fatalf("identity %q", id)
	}
}
