package importer

import (
	"strings"
	"testing"
)

func TestParseDocument(t *testing.T) {
	cases := []struct {
		name, yaml, err string
	}{
		{"без версии", "kind: homedeck\n", "schema_version обязателен"},
		{"версия новее", "schema_version: 99\nkind: homedeck\n", "новее"},
		{"версия ноль", "schema_version: 0\nkind: homedeck\n", "не поддерживается"},
		{"версия строкой", "schema_version: \"1\"\nkind: homedeck\n", "schema_version обязателен"},
		// Неизвестное поле — вероятно опечатка или формат новее; молча его терять нельзя.
		{"неизвестное поле", "schema_version: 1\nkind: homedeck\nwidgetz: []\n", "unknown field"},
		{"неизвестное вложенное поле", "schema_version: 1\nkind: homedeck\nservices:\n  - id: svc_a\n    nmae: x\n", "unknown field"},
		{"другой kind", "schema_version: 1\nkind: homer\n", "kind"},
		{"не отображение", "- a\n- b\n", "ожидается"},
		{"битый YAML", "a: [\n", "YAML"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseDocument([]byte(c.yaml))
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Fatalf("ожидалась ошибка с %q, получено %v", c.err, err)
			}
		})
	}
	doc, err := parseDocument([]byte("schema_version: 1\nkind: homedeck\nsettings:\n  title: Дом\n"))
	if err != nil || doc.Settings.Title != "Дом" {
		t.Fatalf("минимальный документ: %+v %v", doc, err)
	}
}
