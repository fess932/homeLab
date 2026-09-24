package config

import (
	"net/netip"
	"path/filepath"
	"testing"
)

func TestFromEnv(t *testing.T) {
	t.Setenv("HOMEDECK_DATA_DIR", "/srv/hd")
	t.Setenv("HOMEDECK_TRUSTED_PROXIES", "10.0.0.1, 192.168.0.0/16")
	t.Setenv("HOMEDECK_ALLOWED_HOSTS", "Deck.LAN,")
	t.Setenv("HOMEDECK_MIN_FREE_DISK", "1GiB")
	c, err := FromEnv("v1")
	if err != nil {
		t.Fatal(err)
	}
	// Ключ по умолчанию лежит в каталоге данных, чтобы попадать в холодный backup.
	if c.SecretKeyFile != filepath.Join("/srv/hd", "secrets.key") || c.DBPath() != filepath.Join("/srv/hd", "app.db") || c.MinFreeDisk != 1<<30 {
		t.Fatalf("%+v", c)
	}
	if len(c.TrustedProxies) != 2 || !c.TrustedProxies[0].Contains(netip.MustParseAddr("10.0.0.1")) || c.TrustedProxies[0].Bits() != 32 {
		t.Fatalf("proxies: %v", c.TrustedProxies)
	}
	if c.Retention != "50y" {
		t.Fatalf("срок хранения по умолчанию: %s", c.Retention)
	}
	if len(c.AllowedHosts) != 1 || c.AllowedHosts[0] != "deck.lan" {
		t.Fatalf("hosts: %v", c.AllowedHosts)
	}
}

func TestFromEnvErrors(t *testing.T) {
	for _, tc := range []struct{ key, val string }{
		{"HOMEDECK_RETENTION", "30"},
		{"HOMEDECK_RETENTION", "0d"},
		{"HOMEDECK_RETENTION", "30days"},
		{"HOMEDECK_RETENTION", "101y"},
		{"HOMEDECK_RETENTION", "40000d"},
		{"HOMEDECK_MIN_FREE_DISK", "lots"},
		{"HOMEDECK_TRUSTED_PROXIES", "not-an-ip"},
		{"HOMEDECK_VM_MEMORY_PERCENT", "95"},
	} {
		t.Run(tc.key+"="+tc.val, func(t *testing.T) {
			t.Setenv(tc.key, tc.val)
			if _, err := FromEnv("v"); err == nil {
				t.Fatal("ожидалась ошибка")
			}
		})
	}
}
