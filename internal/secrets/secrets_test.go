package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSealOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.key")
	box, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal("sec_a", Payload{Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := box.Open("sec_a", sealed)
	if err != nil || p.Username != "u" || p.Password != "p" {
		t.Fatalf("%+v %v", p, err)
	}
	// id — это AAD: перенос шифротекста в другую запись секрета не должен расшифровываться.
	if _, err := box.Open("sec_b", sealed); err == nil {
		t.Fatal("чужой id открыл секрет")
	}
	if _, err := box.Open("sec_a", sealed[:5]); err == nil {
		t.Fatal("обрезанный шифротекст")
	}

	// Повторная загрузка того же файла даёт тот же ключ.
	box2, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := box2.Open("sec_a", sealed); err != nil || p.Password != "p" {
		t.Fatalf("повторная загрузка: %v", err)
	}

	other, _ := LoadOrCreateKey(filepath.Join(t.TempDir(), "k"))
	if _, err := other.Open("sec_a", sealed); err == nil {
		t.Fatal("другой ключ открыл секрет")
	}
}

func TestBadKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.key")
	if err := os.WriteFile(path, []byte("c2hvcnQ=\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateKey(path); err == nil {
		t.Fatal("короткий ключ")
	}
}
