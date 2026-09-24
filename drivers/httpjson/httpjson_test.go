package httpjson

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/fess932/homeLab/internal/model"
)

func TestLookupAndExtract(t *testing.T) {
	var doc any
	dec := json.NewDecoder(strings.NewReader(`{"meters":[{"power":12.5,"on":true}],"temp":"21.4","name":"x"}`))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	got, missing := extract(doc, []Field{
		{Path: "meters.0.power", Key: "power", Unit: "", Scale: 1},
		{Path: "meters.0.on", Key: "on", Unit: "bool", Scale: 1},
		{Path: "temp", Key: "temperature", Unit: "celsius", Scale: 1},
		{Path: "meters.1.power", Key: "p2", Scale: 1},
		{Path: "name", Key: "name", Scale: 1},
	})
	want := []model.Reading{{Key: "power", Value: 12.5}, {Key: "on", Unit: "bool", Value: 1}, {Key: "temperature", Unit: "celsius", Value: 21.4}}
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %+v, ожидалось %+v", i, got[i], want[i])
		}
	}
	if strings.Join(missing, ",") != "meters.1.power,name" {
		t.Errorf("не найдены: %v", missing)
	}
}

func TestNormalize(t *testing.T) {
	addr, cfg, err := Driver{}.Normalize("http://shelly.lan/status", json.RawMessage(`{"fields":[{"path":" meters.0.power ","key":"power","unit":""}]}`))
	if err != nil || addr != "http://shelly.lan/status" || !strings.Contains(string(cfg), `"path":"meters.0.power"`) || !strings.Contains(string(cfg), `"scale":1`) {
		t.Fatalf("%s %s %v", addr, cfg, err)
	}
	for name, c := range map[string]struct{ addr, cfg string }{
		"address":               {"shelly.lan", `{"fields":[{"path":"a","key":"a"}]}`},
		"config.fields":         {"http://x/", `{"fields":[]}`},
		"config.fields[0].key":  {"http://x/", `{"fields":[{"path":"a","key":"Power W"}]}`},
		"config.fields[1].key":  {"http://x/", `{"fields":[{"path":"a","key":"a"},{"path":"b","key":"a"}]}`},
		"config.fields[0].path": {"http://x/", `{"fields":[{"path":"a..b","key":"a"}]}`},
		"config":                {"http://x/", `{"fieldz":[]}`},
		"config.fields[0].unit": {"http://x/", `{"fields":[{"path":"a","key":"a","unit":"parsec"}]}`},
	} {
		_, _, err := Driver{}.Normalize(c.addr, json.RawMessage(c.cfg))
		ve, ok := errors.AsType[*model.ValidationError](err)
		if !ok || ve.Fields[name] == "" {
			t.Errorf("%s: %v", name, err)
		}
	}
}
