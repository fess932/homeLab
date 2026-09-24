package drivers

import "github.com/fess932/homeLab/internal/model"

// normThresholds — нормы для общих ключей показаний, когда само устройство своих
// порогов тревоги не сообщает. Источники: CO₂ — Umweltbundesamt (до 1000 ppm норма,
// 1000–2000 — пора проветрить, выше 2000 — недопустимо), PM2.5/PM10 — суточные уровни ВОЗ и AQI,
// влажность — комфорт 30–60 %, выше 70 % — риск плесени.
// Пороги с Window "24h" — суточные нормы ВОЗ (2021) для среднего за сутки,
// остальные окрашивают текущее значение.
// Температуры тут нет: датчик может стоять и в комнате, и на улице, и в холодильнике.
var normThresholds = map[string][]model.Threshold{
	"humidity": {
		{Value: 20, Color: "crit", Below: true},
		{Value: 30, Color: "warn", Below: true},
		{Value: 60, Color: "warn"},
		{Value: 70, Color: "crit"},
	},
	"co2":          {{Value: 1000, Color: "warn"}, {Value: 2000, Color: "crit"}},
	"pm1":          {{Value: 25, Color: "warn"}, {Value: 50, Color: "crit"}},
	"pm25":         {{Value: 35, Color: "warn"}, {Value: 75, Color: "crit"}, {Value: 15, Color: "warn", Window: "24h"}},
	"pm10":         {{Value: 50, Color: "warn"}, {Value: 100, Color: "crit"}, {Value: 45, Color: "warn", Window: "24h"}},
	"formaldehyde": {{Value: 0.08, Color: "warn"}, {Value: 0.1, Color: "crit"}},
	"battery":      {{Value: 10, Color: "crit", Below: true}, {Value: 20, Color: "warn", Below: true}},
}

// NormThresholds возвращает нормы для ключа показания или nil.
func NormThresholds(key string) []model.Threshold {
	return normThresholds[key]
}

// WithNorms проставляет нормы показаниям, для которых драйвер не задал своих порогов.
func WithNorms(readings []model.Reading) []model.Reading {
	for i := range readings {
		if readings[i].Thresholds == nil {
			readings[i].Thresholds = NormThresholds(readings[i].Key)
		}
	}
	return readings
}
