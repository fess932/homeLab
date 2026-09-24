// Package tuya — драйвер устройств Tuya: опрос по локальному протоколу (3.3–3.5),
// поиск в локальной сети и подключение аккаунта Smart Life по QR-коду, из
// которого берутся ключи устройств и описание их точек данных.
package tuya

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fess932/homeLab/drivers"
	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/netguard"
)

const (
	Kind        = "tuya"
	DefaultPort = "6668"
)

var (
	versions = []string{"auto", "3.3", "3.4", "3.5"}
	idRe     = regexp.MustCompile(`^[a-zA-Z0-9]{10,32}$`)
	codeRe   = regexp.MustCompile(`^[a-zA-Z0-9_]{1,64}$`)
	dpRe     = regexp.MustCompile(`^[0-9]{1,4}$`)
	dpTypes  = []string{"Integer", "Boolean", "Enum", "String", "Raw", "Bitmap", "Json"}
)

// DP — точка данных устройства: номер, код и формат значения.
type DP struct {
	DP    string   `json:"dp"`
	Code  string   `json:"code"`
	Type  string   `json:"type"`
	Unit  string   `json:"unit"`
	Scale int      `json:"scale"`
	Range []string `json:"range,omitempty"`
}

// Config — настройки устройства Tuya в DeviceInput.Config.
type Config struct {
	DeviceID  string `json:"device_id"`
	Version   string `json:"version"`
	ProductID string `json:"product_id,omitempty"`
	Schema    []DP   `json:"schema"`
}

type Driver struct {
	cloud *cloud
}

func init() {
	drivers.Register(&Driver{cloud: newCloud()})
}

func (d *Driver) Info() drivers.Info {
	return drivers.Info{
		Kind: Kind, Title: "Tuya (локальный протокол)",
		SecretKinds: []string{model.SecretKey}, SecretRequired: true,
		Discover: true, Accounts: true,
	}
}

func (d *Driver) Normalize(address string, raw json.RawMessage) (string, json.RawMessage, error) {
	var c Config
	if err := drivers.Decode(raw, &c); err != nil {
		return "", nil, model.Invalid("config", "настройки Tuya: "+err.Error())
	}
	c.DeviceID = strings.TrimSpace(c.DeviceID)
	if c.Version == "" {
		c.Version = "auto"
	}
	if c.Schema == nil {
		c.Schema = []DP{}
	}
	fields := map[string]string{}
	bad := func(ok bool, field, msg string) {
		if !ok && fields[field] == "" {
			fields[field] = msg
		}
	}
	bad(validAddress(address), "address", "IP-адрес или имя устройства в локальной сети, порт по умолчанию 6668")
	bad(idRe.MatchString(c.DeviceID), "config.device_id", "id устройства из приложения, поиска или JSON Tuya")
	bad(slices.Contains(versions, c.Version), "config.version", "auto, 3.3, 3.4 или 3.5")
	bad(len(c.ProductID) <= 64, "config.product_id", "до 64 символов")
	bad(len(c.Schema) <= 128, "config.schema", "не более 128 точек данных")
	seen := map[string]bool{}
	for i, dp := range c.Schema {
		f := fmt.Sprintf("config.schema[%d]", i)
		bad(dpRe.MatchString(dp.DP) && !seen[dp.DP], f+".dp", "номер точки данных без повторов")
		seen[dp.DP] = true
		bad(codeRe.MatchString(dp.Code), f+".code", "латиница, цифры и _")
		bad(slices.Contains(dpTypes, dp.Type), f+".type", "тип точки данных Tuya")
		bad(dp.Scale >= 0 && dp.Scale <= 9, f+".scale", "от 0 до 9")
		bad(utf8.RuneCountInString(dp.Unit) <= 20 && len(dp.Range) <= 64, f, "слишком длинное описание")
	}
	if len(fields) > 0 {
		return "", nil, &model.ValidationError{Fields: fields}
	}
	out, _ := json.Marshal(c)
	return address, out, nil
}

func validAddress(addr string) bool {
	host, port, err := net.SplitHostPort(withPort(addr))
	return err == nil && host != "" && port != "" && len(addr) <= 255
}

// withPort дополняет адрес стандартным портом локального протокола.
func withPort(addr string) string {
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	return net.JoinHostPort(addr, DefaultPort)
}

func (d *Driver) Poll(ctx context.Context, t drivers.Target) (drivers.Result, error) {
	var c Config
	if err := json.Unmarshal(t.Config, &c); err != nil {
		return drivers.Result{}, drivers.Errorf(drivers.KindParse, "настройки устройства повреждены: "+err.Error())
	}
	version := t.Session
	if version == "" {
		version = c.Version
	}
	dps, used, err := tuyaQuery(ctx, netguard.Dialer(0).DialContext, withPort(t.Address), c.DeviceID, []byte(t.Secret.Key), version)
	if err != nil {
		return drivers.Result{}, err
	}
	return drivers.Result{Readings: withAlarmLimits(readings(dps, c.Schema)), Session: used, Protocol: used}, nil
}

func (d *Driver) Discover(ctx context.Context, opts drivers.DiscoverOptions) (drivers.DiscoverResult, error) {
	var subnets []netip.Prefix
	if opts.Subnet != "" {
		p, err := netip.ParsePrefix(strings.TrimSpace(opts.Subnet))
		if err != nil || !p.Addr().Is4() || p.Bits() < 16 {
			return drivers.DiscoverResult{}, model.Invalid("subnet", "подсеть IPv4 вида 192.168.0.0/24, не шире /16")
		}
		subnets = []netip.Prefix{p.Masked()}
	}
	wait := opts.Wait
	if wait <= 0 {
		wait = 6 * time.Second
	}
	found := discoverLAN(ctx, subnets, wait)
	res := drivers.DiscoverResult{Candidates: []drivers.Candidate{}, Subnets: found.Subnets, Warnings: found.Warnings}
	if !found.Broadcast {
		res.Warnings = append(res.Warnings, "UDP-анонсы не слушаются: устройства найдутся только по открытому порту, без id")
	}
	for _, f := range found.Devices {
		cfg, _ := json.Marshal(Config{DeviceID: f.DeviceID, Version: versionOrAuto(f.Version), ProductID: f.ProductID, Schema: []DP{}})
		c := drivers.Candidate{Address: f.IP, Config: cfg, ProductID: f.ProductID, Ref: f.DeviceID, InNetwork: true}
		if f.Via == "scan" {
			c.Note = "найден только открытый порт 6668: id устройства неизвестен"
		}
		res.Candidates = append(res.Candidates, c)
	}
	return res, nil
}

// Identity — id устройства Tuya: по нему совпадают находки в сети, в аккаунте и добавленные устройства.
func (d *Driver) Identity(config json.RawMessage) string {
	var c Config
	_ = json.Unmarshal(config, &c)
	return c.DeviceID
}

func versionOrAuto(v string) string {
	if slices.Contains(versions, v) {
		return v
	}
	return "auto"
}

// Общие ключи величин: одинаковые для любых устройств, чтобы один запрос
// «температура» работал для датчиков разных производителей.
var canonicalKeys = map[string]string{
	"temp_current":       "temperature",
	"temp_value":         "temperature",
	"va_temperature":     "temperature",
	"temperature":        "temperature",
	"humidity_value":     "humidity",
	"va_humidity":        "humidity",
	"humidity":           "humidity",
	"co2_value":          "co2",
	"co2":                "co2",
	"pm25_value":         "pm25",
	"pm25":               "pm25",
	"pm1_value":          "pm1",
	"pm1":                "pm1",
	"pm10_value":         "pm10",
	"pm10":               "pm10",
	"ch2o_value":         "formaldehyde",
	"voc_value":          "voc",
	"battery_percentage": "battery",
	"battery_value":      "battery",
	"charge_state":       "charging",
	"air_quality_index":  "air_quality",
}

// readings переводит точки данных в значения HomeDeck. Точки из схемы устройства
// получают общий ключ, единицу и масштаб; остальные отдаются как есть под ключом
// dp_<номер>, чтобы по ним можно было понять, что они значат.
func readings(dps map[string]any, schema []DP) []model.Reading {
	byDP := map[string]DP{}
	for _, s := range schema {
		byDP[s.DP] = s
	}
	out := []model.Reading{}
	for dp, raw := range dps {
		s, known := byDP[dp]
		if !known {
			if r, ok := rawReading("dp_"+dp, raw); ok {
				out = append(out, r)
			}
			continue
		}
		key := canonicalKeys[s.Code]
		if key == "" {
			key = strings.ToLower(s.Code)
		}
		switch s.Type {
		case "Integer":
			if v, ok := drivers.Number(raw); ok {
				out = append(out, model.Reading{Key: key, Unit: drivers.CanonicalUnit(s.Unit), Value: v / math.Pow10(s.Scale)})
			}
		case "Boolean":
			if b, ok := raw.(bool); ok {
				out = append(out, model.Reading{Key: key, Unit: "bool", Value: drivers.BoolValue(b)})
			}
		case "Enum":
			if st, ok := raw.(string); ok {
				// Числом служит позиция в перечислении с единицы: level_1 → 1.
				out = append(out, model.Reading{Key: key, Value: float64(slices.Index(s.Range, st) + 1), State: drivers.Clip(st)})
			}
		}
	}
	slices.SortFunc(out, func(a, b model.Reading) int { return strings.Compare(a.Key, b.Key) })
	return out
}

// alarmLimits — точки с порогами тревоги, которые пользователь задаёт в приложении
// (датчики температуры и влажности): показание → коды нижнего и верхнего порога.
// У Tuya встречаются оба написания: minitemp_set и mintemp_set.
var alarmLimits = map[string]struct{ low, high []string }{
	"temperature": {low: []string{"minitemp_set", "mintemp_set"}, high: []string{"maxtemp_set"}},
	"humidity":    {low: []string{"minihum_set", "minhum_set"}, high: []string{"maxhum_set"}},
}

// withAlarmLimits превращает пороги тревоги устройства в пороги показаний:
// они важнее типовых норм, потому что их выставил сам пользователь.
func withAlarmLimits(rs []model.Reading) []model.Reading {
	values := map[string]float64{}
	for _, r := range rs {
		values[r.Key] = r.Value
	}
	pick := func(codes []string) (float64, bool) {
		for _, c := range codes {
			if v, ok := values[c]; ok {
				return v, true
			}
		}
		return 0, false
	}
	for i, r := range rs {
		lim, ok := alarmLimits[r.Key]
		if !ok {
			continue
		}
		var th []model.Threshold
		if v, ok := pick(lim.low); ok {
			th = append(th, model.Threshold{Value: v, Color: "crit", Below: true})
		}
		if v, ok := pick(lim.high); ok {
			th = append(th, model.Threshold{Value: v, Color: "crit"})
		}
		if th != nil {
			rs[i].Thresholds = th
		}
	}
	return rs
}

func rawReading(key string, raw any) (model.Reading, bool) {
	switch v := raw.(type) {
	case bool:
		return model.Reading{Key: key, Unit: "bool", Value: drivers.BoolValue(v)}, true
	case string:
		return model.Reading{Key: key, State: drivers.Clip(v)}, v != ""
	}
	if f, ok := drivers.Number(raw); ok {
		return model.Reading{Key: key, Value: f}, true
	}
	return model.Reading{}, false
}
