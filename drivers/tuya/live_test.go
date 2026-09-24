package tuya

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/fess932/homeLab/internal/netguard"
)

// Проверка на настоящем устройстве Tuya. Запускается, только если заданы
// HOMEDECK_TUYA_ADDR, HOMEDECK_TUYA_ID и HOMEDECK_TUYA_KEY (и, по желанию, HOMEDECK_TUYA_VERSION).
func TestTuyaLive(t *testing.T) {
	addr, id, key := os.Getenv("HOMEDECK_TUYA_ADDR"), os.Getenv("HOMEDECK_TUYA_ID"), os.Getenv("HOMEDECK_TUYA_KEY")
	if addr == "" || id == "" || key == "" {
		t.Skip("HOMEDECK_TUYA_ADDR/ID/KEY не заданы")
	}
	version := os.Getenv("HOMEDECK_TUYA_VERSION")
	if version == "" {
		version = "auto"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dps, used, err := tuyaQuery(ctx, netguard.Dialer(0).DialContext, addr, id, []byte(key), version)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("версия %s, точки данных: %v", used, dps)
	if len(dps) == 0 {
		t.Fatal("устройство не вернуло точек данных")
	}
}
