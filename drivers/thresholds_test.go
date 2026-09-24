package drivers

import (
	"testing"

	"github.com/fess932/homeLab/internal/model"
)

func TestWithNorms(t *testing.T) {
	own := []model.Threshold{{Value: 25, Color: "crit"}}
	rs := WithNorms([]model.Reading{
		{Key: "humidity", Value: 44},
		{Key: "temperature", Value: 23, Thresholds: own},
		{Key: "power", Value: 5},
	})
	if len(rs[0].Thresholds) != 4 || !rs[0].Thresholds[0].Below {
		t.Fatalf("влажность без норм: %+v", rs[0].Thresholds)
	}
	if len(rs[1].Thresholds) != 1 || rs[1].Thresholds[0].Value != 25 {
		t.Fatalf("свои пороги драйвера затёрты: %+v", rs[1].Thresholds)
	}
	if rs[2].Thresholds != nil {
		t.Fatalf("у неизвестного ключа нормы: %+v", rs[2].Thresholds)
	}
	var daily bool
	for _, th := range NormThresholds("pm25") {
		daily = daily || (th.Window == "24h" && th.Value == 15)
	}
	if !daily {
		t.Fatal("PM2.5: нет суточной нормы ВОЗ 15 мкг/м³")
	}
	h := model.Threshold{Value: 30, Below: true}
	if !h.Hit(30) || h.Hit(31) {
		t.Fatal("нижний порог")
	}
}
