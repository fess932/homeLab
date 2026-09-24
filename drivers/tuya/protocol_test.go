package tuya

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"hash/crc32"
	"io"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fess932/homeLab/drivers"
	"github.com/fess932/homeLab/internal/model"
)

const testKey = "0123456789abcdef"

// fakeTuya — сторона устройства локального протокола. Написана независимо от клиента:
// кадры собираются и разбираются вручную, чтобы ошибка клиента не повторилась в проверке.
type fakeTuya struct {
	t       *testing.T // nil — устройство молча рвёт соединение на чужие кадры, как настоящее
	version string
	dps     string
	seq     uint32
}

func (f *fakeTuya) errorf(format string, args ...any) {
	if f.t != nil {
		f.errorf(format, args...)
	}
}

func (f *fakeTuya) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	real := []byte(testKey)
	switch f.version {
	case "3.3":
		cmd, plain := f.read55AA(conn, real, false)
		if cmd != cmdDPQuery || !bytes.Contains(plain, []byte(`"devId":"dev123456789"`)) {
			f.errorf("3.3: неожиданный запрос %x %q", cmd, plain)
			return
		}
		f.write55AA(conn, real, cmdDPQuery, append([]byte("3.3\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"), ecbEncrypt(real, []byte(f.dps))...), false)
	case "3.4", "3.5":
		read := func(key []byte) (uint32, []byte) {
			if f.version == "3.5" {
				return f.read6699(conn, key)
			}
			return f.read55AA(conn, key, true)
		}
		write := func(key []byte, cmd uint32, plain []byte) {
			if f.version == "3.5" {
				f.write6699(conn, key, cmd, plain)
				return
			}
			f.write55AA(conn, key, cmd, ecbEncrypt(key, plain), true)
		}
		cmd, local := read(real)
		if cmd != cmdSessKeyStart || len(local) != 16 {
			f.errorf("%s: ожидалось начало согласования, получено %x", f.version, cmd)
			return
		}
		remote := make([]byte, 16)
		_, _ = rand.Read(remote)
		write(real, cmdSessKeyResp, append(remote, hmacSHA256(real, local)...))
		cmd, proof := read(real)
		if cmd != cmdSessKeyFinish || !bytes.Equal(proof, hmacSHA256(real, remote)) {
			f.errorf("%s: клиент не подтвердил ключ", f.version)
			return
		}
		x := make([]byte, 16)
		for i := range x {
			x[i] = local[i] ^ remote[i]
		}
		var session []byte
		if f.version == "3.4" {
			session = ecbEncryptRaw(real, x)
		} else {
			g, _ := cipher.NewGCM(mustAES(real))
			session = g.Seal(nil, local[:12], x, nil)[:16]
		}
		cmd, body := read(session)
		if cmd != cmdDPQueryNew || string(body) != "{}" {
			f.errorf("%s: неожиданный запрос %x %q", f.version, cmd, body)
			return
		}
		// Перед ответом устройство может прислать служебный кадр — клиент должен его пропустить.
		write(session, 0x09, []byte{})
		write(session, cmdDPQueryNew, []byte(f.dps))
	}
}

func (f *fakeTuya) read55AA(conn net.Conn, key []byte, useHMAC bool) (uint32, []byte) {
	hdr := make([]byte, 16)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		f.errorf("чтение заголовка: %v", err)
		return 0, nil
	}
	body := make([]byte, binary.BigEndian.Uint32(hdr[12:]))
	if _, err := io.ReadFull(conn, body); err != nil {
		f.errorf("чтение кадра: %v", err)
		return 0, nil
	}
	end := 8
	if useHMAC {
		end = 36
		if !bytes.Equal(body[len(body)-36:len(body)-4], hmacSHA256(key, append(hdr, body[:len(body)-36]...))) {
			f.errorf("HMAC кадра клиента не сходится")
		}
	}
	plain, err := ecbDecrypt(key, body[:len(body)-end])
	if err != nil {
		f.errorf("расшифровка кадра клиента: %v", err)
	}
	return binary.BigEndian.Uint32(hdr[8:]), plain
}

func (f *fakeTuya) write55AA(conn net.Conn, key []byte, cmd uint32, payload []byte, useHMAC bool) {
	end := 8
	if useHMAC {
		end = 36
	}
	f.seq++
	frame := binary.BigEndian.AppendUint32(nil, prefix55AA)
	frame = binary.BigEndian.AppendUint32(frame, f.seq)
	frame = binary.BigEndian.AppendUint32(frame, cmd)
	frame = binary.BigEndian.AppendUint32(frame, uint32(4+len(payload)+end))
	frame = append(frame, 0, 0, 0, 0) // код возврата
	frame = append(frame, payload...)
	if useHMAC {
		frame = append(frame, hmacSHA256(key, frame)...)
	} else {
		frame = binary.BigEndian.AppendUint32(frame, crc32.ChecksumIEEE(frame))
	}
	_, _ = conn.Write(binary.BigEndian.AppendUint32(frame, suffix55AA))
}

func (f *fakeTuya) read6699(conn net.Conn, key []byte) (uint32, []byte) {
	hdr := make([]byte, 18)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		f.errorf("чтение заголовка: %v", err)
		return 0, nil
	}
	body := make([]byte, binary.BigEndian.Uint32(hdr[14:])+4)
	if _, err := io.ReadFull(conn, body); err != nil {
		f.errorf("чтение кадра: %v", err)
		return 0, nil
	}
	g, _ := cipher.NewGCM(mustAES(key))
	plain, err := g.Open(nil, body[:12], body[12:len(body)-4], hdr[4:])
	if err != nil {
		f.errorf("расшифровка кадра клиента: %v", err)
	}
	return binary.BigEndian.Uint32(hdr[10:]), plain
}

func (f *fakeTuya) write6699(conn net.Conn, key []byte, cmd uint32, payload []byte) {
	f.seq++
	plain := append([]byte{0, 0, 0, 0}, payload...) // код возврата
	iv := make([]byte, 12)
	_, _ = rand.Read(iv)
	hdr := binary.BigEndian.AppendUint32(nil, prefix6699)
	hdr = append(hdr, 0, 0)
	hdr = binary.BigEndian.AppendUint32(hdr, f.seq)
	hdr = binary.BigEndian.AppendUint32(hdr, cmd)
	hdr = binary.BigEndian.AppendUint32(hdr, uint32(12+len(plain)+16))
	b, _ := aes.NewCipher(key)
	g, _ := cipher.NewGCM(b)
	frame := append(append([]byte{}, hdr...), iv...)
	frame = g.Seal(frame, iv, plain, hdr[4:])
	_, _ = conn.Write(binary.BigEndian.AppendUint32(frame, suffix6699))
}

func startFake(t *testing.T, f *fakeTuya) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return ln.Addr().String()
}

var plainDial = (&net.Dialer{}).DialContext

func TestTuyaVersions(t *testing.T) {
	for _, v := range []string{"3.3", "3.4", "3.5"} {
		t.Run(v, func(t *testing.T) {
			addr := startFake(t, &fakeTuya{t: t, version: v, dps: `{"dps":{"2":23,"4":639,"23":true}}`})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			dps, used, err := tuyaQuery(ctx, plainDial, addr, "dev123456789", []byte(testKey), v)
			if err != nil {
				t.Fatal(err)
			}
			if used != v || len(dps) != 3 || dps["23"] != true {
				t.Fatalf("версия %s, точки %v", used, dps)
			}
		})
	}
}

func TestTuyaAutoDetect(t *testing.T) {
	// Устройство 3.3 закрывает соединение на чужие кадры — автоопределение должно дойти до 3.3.
	addr := startFake(t, &fakeTuya{version: "3.3", dps: `{"dps":{"1":"level_2"}}`})
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	dps, used, err := tuyaQuery(ctx, plainDial, addr, "dev123456789", []byte(testKey), "auto")
	if err != nil || used != "3.3" || dps["1"] != "level_2" {
		t.Fatalf("версия %q, точки %v, ошибка %v", used, dps, err)
	}
}

func TestTuyaWrongKey(t *testing.T) {
	addr := startFake(t, &fakeTuya{version: "3.5", dps: `{"dps":{}}`})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := tuyaQuery(ctx, plainDial, addr, "dev123456789", []byte("fedcba9876543210"), "3.5")
	kind, msg := drivers.Classify(err)
	if kind != drivers.KindAuth && kind != drivers.KindProtocol {
		t.Fatalf("неверный ключ должен давать ошибку ключа или протокола: %s %q", kind, msg)
	}
	if _, _, err := tuyaQuery(ctx, plainDial, addr, "dev123456789", []byte("short"), "3.5"); err == nil || !strings.Contains(err.Error(), "16 символов") {
		t.Fatalf("короткий ключ: %v", err)
	}
}

func TestTuyaDialError(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	_, _, err := tuyaQuery(ctx, plainDial, addr, "dev123456789", []byte(testKey), "auto")
	if kind, _ := drivers.Classify(err); kind != "connect" {
		t.Fatalf("ожидалась ошибка соединения: %v", err)
	}
	// Перебор версий не должен повторять заведомо безуспешное подключение до таймаута.
	if time.Since(start) > 2*time.Second {
		t.Fatalf("ошибка соединения обработана за %s", time.Since(start))
	}
}

// Схема и ответ датчика качества воздуха MT15/MT29 (Tuya, протокол 3.5).
var airSchema = []DP{
	{DP: "1", Code: "air_quality_index", Type: "Enum", Range: []string{"level_1", "level_2", "level_3"}},
	{DP: "2", Code: "temp_current", Type: "Integer", Unit: "℃"},
	{DP: "3", Code: "humidity_value", Type: "Integer", Unit: "%"},
	{DP: "4", Code: "co2_value", Type: "Integer", Unit: "ppm"},
	{DP: "5", Code: "ch2o_value", Type: "Integer", Unit: "mg/m³", Scale: 3},
	{DP: "7", Code: "pm25_value", Type: "Integer", Unit: "ug/m³"},
	{DP: "22", Code: "battery_percentage", Type: "Integer", Unit: "%"},
	{DP: "23", Code: "charge_state", Type: "Boolean"},
	{DP: "28", Code: "alarm_volume", Type: "Enum", Range: []string{"low", "middle", "high", "mute"}},
}

func TestTuyaReadings(t *testing.T) {
	dps, ok := dpsFromPayload([]byte(`{"dps":{"1":"level_1","2":23,"3":44,"4":628,"5":3,"7":6,"22":100,"23":true,"28":"middle","101":298,"112":"c","106":false}}`))
	if !ok {
		t.Fatal("dps не разобраны")
	}
	got := map[string]model.Reading{}
	for _, r := range readings(dps, airSchema) {
		got[r.Key] = r
	}
	want := map[string]model.Reading{
		"air_quality":  {Key: "air_quality", Value: 1, State: "level_1"},
		"temperature":  {Key: "temperature", Unit: "celsius", Value: 23},
		"humidity":     {Key: "humidity", Unit: "percent", Value: 44},
		"co2":          {Key: "co2", Unit: "ppm", Value: 628},
		"formaldehyde": {Key: "formaldehyde", Unit: "mgm3", Value: 0.003},
		"pm25":         {Key: "pm25", Unit: "ugm3", Value: 6},
		"battery":      {Key: "battery", Unit: "percent", Value: 100},
		"charging":     {Key: "charging", Unit: "bool", Value: 1},
		"alarm_volume": {Key: "alarm_volume", Value: 2, State: "middle"},
		"dp_101":       {Key: "dp_101", Value: 298},
		"dp_106":       {Key: "dp_106", Unit: "bool", Value: 0},
		"dp_112":       {Key: "dp_112", State: "c"},
	}
	for k, w := range want {
		if !reflect.DeepEqual(got[k], w) {
			t.Errorf("%s: %+v, ожидалось %+v", k, got[k], w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("лишние значения: %v", got)
	}
}

func TestDPSFromPayload(t *testing.T) {
	if dps, ok := dpsFromPayload([]byte(`{"data":{"dps":{"1":true}},"t":1}`)); !ok || dps["1"] != true {
		t.Fatalf("формат 3.4 с data.dps: %v", dps)
	}
	if _, ok := dpsFromPayload([]byte("data unvalid")); ok {
		t.Fatal("не JSON не должен разбираться")
	}
}
