// Package httpjson — драйвер устройств с HTTP API, которые отдают JSON (Shelly,
// ESPHome, самодельные): значения берутся по путям через точку.
package httpjson

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/fess932/homeLab/drivers"
	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/netguard"
	"github.com/fess932/homeLab/internal/probe"
)

const (
	Kind        = "http_json"
	maxJSONBody = 1 << 20
)

var (
	keyRe  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	pathRe = regexp.MustCompile(`^[^.\s]+(\.[^.\s]+)*$`)
)

// Field — значение из ответа: путь через точку (массивы — номером), ключ метрики,
// единица HomeDeck и множитель.
type Field struct {
	Path  string  `json:"path"`
	Key   string  `json:"key"`
	Unit  string  `json:"unit"`
	Scale float64 `json:"scale"`
}

type Config struct {
	Fields []Field `json:"fields"`
}

type Driver struct{}

func init() {
	drivers.Register(Driver{})
}

func (Driver) Info() drivers.Info {
	return drivers.Info{Kind: Kind, Title: "HTTP JSON", SecretKinds: []string{"basic", "bearer"}}
}

func (Driver) Normalize(address string, raw json.RawMessage) (string, json.RawMessage, error) {
	var c Config
	if err := drivers.Decode(raw, &c); err != nil {
		return "", nil, model.Invalid("config", "настройки HTTP JSON: "+err.Error())
	}
	fields := map[string]string{}
	bad := func(ok bool, field, msg string) {
		if !ok && fields[field] == "" {
			fields[field] = msg
		}
	}
	u, err := url.Parse(address)
	bad(err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.Fragment == "",
		"address", "адрес http:// или https:// без логина и пароля; учётные данные задаются отдельно")
	bad(len(c.Fields) >= 1 && len(c.Fields) <= 64, "config.fields", "от 1 до 64 полей")
	keys := map[string]bool{}
	for i := range c.Fields {
		fl := &c.Fields[i]
		fl.Path, fl.Key = strings.TrimSpace(fl.Path), strings.TrimSpace(fl.Key)
		if fl.Scale == 0 {
			fl.Scale = 1
		}
		f := fmt.Sprintf("config.fields[%d]", i)
		bad(len(fl.Path) <= 256 && pathRe.MatchString(fl.Path), f+".path", "путь через точку, например sensors.0.temp")
		bad(keyRe.MatchString(fl.Key) && !keys[fl.Key], f+".key", "латиница в нижнем регистре, цифры и _, без повторов")
		keys[fl.Key] = true
		bad(slices.Contains(model.Units, fl.Unit), f+".unit", "неизвестная единица")
	}
	if len(fields) > 0 {
		return "", nil, &model.ValidationError{Fields: fields}
	}
	out, _ := json.Marshal(c)
	return address, out, nil
}

func (Driver) Poll(ctx context.Context, t drivers.Target) (drivers.Result, error) {
	var c Config
	if err := json.Unmarshal(t.Config, &c); err != nil {
		return drivers.Result{}, drivers.Errorf(drivers.KindParse, "настройки устройства повреждены: "+err.Error())
	}
	tr := netguard.Transport(nil)
	tr.DisableKeepAlives = true
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, CheckRedirect: netguard.NoRedirects}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.Address, nil)
	if err != nil {
		return drivers.Result{}, err
	}
	req.Header.Set("Accept", "application/json")
	switch {
	case t.Secret.Token != "":
		req.Header.Set("Authorization", "Bearer "+t.Secret.Token)
	case t.Secret.Username != "":
		req.SetBasicAuth(t.Secret.Username, t.Secret.Password)
	}
	resp, err := client.Do(req)
	if err != nil {
		return drivers.Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return drivers.Result{}, drivers.Errorf(probe.KindHTTPStatus, fmt.Sprintf("устройство ответило HTTP %d", resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONBody+1))
	if err != nil {
		return drivers.Result{}, err
	}
	if len(body) > maxJSONBody {
		return drivers.Result{}, drivers.Errorf(drivers.KindParse, "ответ больше 1 МиБ")
	}
	var doc any
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return drivers.Result{}, drivers.Errorf(drivers.KindParse, "ответ не JSON: "+err.Error())
	}
	readings, missing := extract(doc, c.Fields)
	if len(readings) == 0 {
		return drivers.Result{}, drivers.Errorf(drivers.KindParse, "в ответе нет ни одного числового поля из настроек: "+strings.Join(missing, ", "))
	}
	return drivers.Result{Readings: readings}, nil
}

// extract достаёт значения полей из ответа; missing — пути, которых нет или они не числа.
func extract(doc any, fields []Field) (out []model.Reading, missing []string) {
	out = []model.Reading{}
	for _, f := range fields {
		v, ok := lookup(doc, f.Path)
		if !ok {
			missing = append(missing, f.Path)
			continue
		}
		if b, isBool := v.(bool); isBool {
			out = append(out, model.Reading{Key: f.Key, Unit: f.Unit, Value: drivers.BoolValue(b)})
			continue
		}
		n, ok := drivers.Number(v)
		if !ok {
			missing = append(missing, f.Path)
			continue
		}
		out = append(out, model.Reading{Key: f.Key, Unit: f.Unit, Value: n * f.Scale})
	}
	return out, missing
}

// lookup идёт по пути через точку: имена полей объектов и номера элементов массивов.
func lookup(doc any, path string) (any, bool) {
	cur := doc
	for part := range strings.SplitSeq(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[part]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(node) {
				return nil, false
			}
			cur = node[i]
		default:
			return nil, false
		}
	}
	return cur, true
}
