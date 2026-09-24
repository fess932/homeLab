package tuya

import (
	"context"
	"os"
	"testing"
	"time"
)

// Поиск в настоящей сети. Запускается только при HOMEDECK_DISCOVERY_LIVE=1.
func TestDiscoverLive(t *testing.T) {
	if os.Getenv("HOMEDECK_DISCOVERY_LIVE") == "" {
		t.Skip("HOMEDECK_DISCOVERY_LIVE не задан")
	}
	start := time.Now()
	res := discoverLAN(context.Background(), nil, 6*time.Second)
	t.Logf("за %s: подсети %v, broadcast %v, предупреждения %v", time.Since(start).Round(time.Millisecond), res.Subnets, res.Broadcast, res.Warnings)
	for _, f := range res.Devices {
		t.Logf("  %+v", f)
	}
}
