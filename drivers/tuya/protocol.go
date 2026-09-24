package tuya

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/fess932/homeLab/drivers"
)

// Локальный протокол Tuya (порт 6668). Версии 3.3 и 3.4 используют кадры 0x55AA
// с AES-ECB (3.4 — с сессионным ключом и HMAC), 3.5 — кадры 0x6699 с AES-GCM,
// где заголовок кадра служит дополнительными аутентифицированными данными.
const (
	cmdSessKeyStart  = 0x03
	cmdSessKeyResp   = 0x04
	cmdSessKeyFinish = 0x05
	cmdDPQuery       = 0x0a
	cmdDPQueryNew    = 0x10

	prefix55AA = 0x000055AA
	suffix55AA = 0x0000AA55
	prefix6699 = 0x00006699
	suffix6699 = 0x00009966

	maxTuyaFrame = 64 << 10
)

type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// dialError — до устройства не удалось даже подключиться: перебор версий не поможет.
type dialError struct{ err error }

func (e *dialError) Error() string { return e.err.Error() }
func (e *dialError) Unwrap() error { return e.err }

// Порядок перебора при version=auto: сначала новые версии, их больше среди свежих устройств.
var tuyaAutoOrder = []string{"3.5", "3.4", "3.3"}

type tuyaSession struct {
	conn    net.Conn
	version string
	realKey []byte
	key     []byte
	seq     uint32
}

// tuyaQuery читает текущие значения точек данных устройства и возвращает версию
// протокола, с которой это удалось.
func tuyaQuery(ctx context.Context, dial dialFunc, addr, devID string, localKey []byte, version string) (map[string]any, string, error) {
	if len(localKey) != 16 {
		return nil, "", drivers.Errorf(drivers.KindAuth, "local_key должен быть длиной 16 символов")
	}
	if version != "auto" {
		dps, err := tuyaQueryVersion(ctx, dial, addr, devID, localKey, version)
		return dps, version, err
	}
	// Устройство другой версии обычно молча закрывает соединение или не отвечает,
	// поэтому перебор продолжается на любой ошибке, кроме невозможности подключиться,
	// а оставшееся время делится между попытками.
	for i, v := range tuyaAutoOrder {
		actx, cancel := ctx, context.CancelFunc(func() {})
		if dl, ok := ctx.Deadline(); ok {
			actx, cancel = context.WithTimeout(ctx, time.Until(dl)/time.Duration(len(tuyaAutoOrder)-i))
		}
		dps, err := tuyaQueryVersion(actx, dial, addr, devID, localKey, v)
		cancel()
		if err == nil {
			return dps, v, nil
		}
		if de := (*dialError)(nil); errors.As(err, &de) || ctx.Err() != nil {
			return nil, "", err
		}
	}
	return nil, "", drivers.Errorf(drivers.KindAuth, "устройство не ответило ни по одной версии протокола 3.3–3.5: проверьте local_key и id устройства")
}

func tuyaQueryVersion(ctx context.Context, dial dialFunc, addr, devID string, localKey []byte, version string) (map[string]any, error) {
	conn, err := dial(ctx, "tcp", addr)
	if err != nil {
		return nil, &dialError{err}
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	s := &tuyaSession{conn: conn, version: version, realKey: localKey, key: localKey, seq: 1}

	switch version {
	case "3.3":
		payload, _ := json.Marshal(map[string]string{"gwId": devID, "devId": devID, "uid": devID, "t": strconv.FormatInt(time.Now().Unix(), 10)})
		if err := s.write55AA(cmdDPQuery, ecbEncrypt(s.key, payload), false); err != nil {
			return nil, err
		}
	case "3.4", "3.5":
		if err := s.negotiate(); err != nil {
			return nil, err
		}
		if err := s.send(cmdDPQueryNew, []byte("{}")); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("версия протокола %s не поддерживается", version)
	}

	// Устройство может прислать служебные кадры раньше ответа — ждём первый с точками данных.
	for {
		cmd, plain, err := s.read()
		if err != nil {
			return nil, err
		}
		dps, ok := dpsFromPayload(plain)
		if ok {
			return dps, nil
		}
		if cmd == cmdDPQuery && bytes.Contains(plain, []byte("data unvalid")) {
			return nil, drivers.Errorf(drivers.KindProtocol, "устройство не поддерживает запрос состояния (device22)")
		}
	}
}

// negotiate согласует сессионный ключ (3.4 и 3.5): обмен случайными nonce,
// взаимная проверка через HMAC от local_key и вывод ключа из XOR nonce.
func (s *tuyaSession) negotiate() error {
	local := make([]byte, 16)
	if _, err := rand.Read(local); err != nil {
		return err
	}
	if err := s.send(cmdSessKeyStart, local); err != nil {
		return err
	}
	var resp []byte
	for {
		cmd, plain, err := s.read()
		if err != nil {
			return err
		}
		if cmd == cmdSessKeyResp {
			resp = plain
			break
		}
	}
	if len(resp) < 48 {
		return drivers.Errorf(drivers.KindAuth, "устройство не приняло согласование ключа: проверьте local_key")
	}
	remote := resp[:16]
	if !hmac.Equal(hmacSHA256(s.realKey, local), resp[16:48]) {
		return drivers.Errorf(drivers.KindAuth, "неверный local_key: устройство подписало ответ другим ключом")
	}
	if err := s.send(cmdSessKeyFinish, hmacSHA256(s.realKey, remote)); err != nil {
		return err
	}
	x := make([]byte, 16)
	for i := range x {
		x[i] = local[i] ^ remote[i]
	}
	if s.version == "3.4" {
		s.key = ecbEncryptRaw(s.realKey, x)
		return nil
	}
	gcm, _ := cipher.NewGCM(mustAES(s.realKey))
	s.key = gcm.Seal(nil, local[:12], x, nil)[:16]
	return nil
}

// send шифрует и отправляет команду текущим ключом сессии.
func (s *tuyaSession) send(cmd uint32, payload []byte) error {
	if s.version == "3.5" {
		return s.write6699(cmd, payload)
	}
	return s.write55AA(cmd, ecbEncrypt(s.key, payload), true)
}

func (s *tuyaSession) write55AA(cmd uint32, payload []byte, useHMAC bool) error {
	endLen := 8
	if useHMAC {
		endLen = 36
	}
	buf := make([]byte, 16, 16+len(payload)+endLen)
	binary.BigEndian.PutUint32(buf[0:], prefix55AA)
	binary.BigEndian.PutUint32(buf[4:], s.nextSeq())
	binary.BigEndian.PutUint32(buf[8:], cmd)
	binary.BigEndian.PutUint32(buf[12:], uint32(len(payload)+endLen))
	buf = append(buf, payload...)
	if useHMAC {
		buf = append(buf, hmacSHA256(s.key, buf)...)
	} else {
		buf = binary.BigEndian.AppendUint32(buf, crc32.ChecksumIEEE(buf))
	}
	buf = binary.BigEndian.AppendUint32(buf, suffix55AA)
	_, err := s.conn.Write(buf)
	return err
}

func (s *tuyaSession) write6699(cmd uint32, payload []byte) error {
	frame, err := frame6699(s.key, s.nextSeq(), cmd, payload)
	if err != nil {
		return err
	}
	_, err = s.conn.Write(frame)
	return err
}

// frame6699 собирает кадр 0x6699: заголовок, случайный IV, AES-GCM с заголовком
// в качестве дополнительных данных и суффикс.
func frame6699(key []byte, seq, cmd uint32, payload []byte) ([]byte, error) {
	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}
	hdr := make([]byte, 18)
	binary.BigEndian.PutUint32(hdr[0:], prefix6699)
	binary.BigEndian.PutUint32(hdr[6:], seq)
	binary.BigEndian.PutUint32(hdr[10:], cmd)
	binary.BigEndian.PutUint32(hdr[14:], uint32(12+len(payload)+16))
	gcm, _ := cipher.NewGCM(mustAES(key))
	buf := append(hdr, iv...)
	buf = gcm.Seal(buf, iv, payload, hdr[4:])
	return binary.BigEndian.AppendUint32(buf, suffix6699), nil
}

// read принимает один кадр и возвращает команду и расшифрованные данные без кода возврата.
func (s *tuyaSession) read() (uint32, []byte, error) {
	var pfx [4]byte
	if _, err := io.ReadFull(s.conn, pfx[:]); err != nil {
		return 0, nil, err
	}
	switch binary.BigEndian.Uint32(pfx[:]) {
	case prefix55AA:
		return s.read55AA(pfx[:])
	case prefix6699:
		return s.read6699(pfx[:])
	}
	return 0, nil, drivers.Errorf(drivers.KindProtocol, fmt.Sprintf("неизвестный формат кадра %x: это не локальный протокол Tuya", pfx))
}

func (s *tuyaSession) read55AA(pfx []byte) (uint32, []byte, error) {
	hdr := make([]byte, 16)
	copy(hdr, pfx)
	if _, err := io.ReadFull(s.conn, hdr[4:]); err != nil {
		return 0, nil, err
	}
	cmd, n := binary.BigEndian.Uint32(hdr[8:]), binary.BigEndian.Uint32(hdr[12:])
	endLen := 8
	if s.version != "3.3" {
		endLen = 36
	}
	if n < uint32(4+endLen) || n > maxTuyaFrame {
		return 0, nil, drivers.Errorf(drivers.KindProtocol, "повреждённый кадр")
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(s.conn, body); err != nil {
		return 0, nil, err
	}
	signed := append(hdr, body[:len(body)-endLen]...)
	check := body[len(body)-endLen : len(body)-4]
	if endLen == 36 {
		// Ответ на начало согласования ещё подписан исходным ключом.
		if !hmac.Equal(check, hmacSHA256(s.key, signed)) && !hmac.Equal(check, hmacSHA256(s.realKey, signed)) {
			return 0, nil, drivers.Errorf(drivers.KindAuth, "подпись кадра не сходится: неверный local_key или версия протокола")
		}
	} else if binary.BigEndian.Uint32(check) != crc32.ChecksumIEEE(signed) {
		return 0, nil, drivers.Errorf(drivers.KindProtocol, "контрольная сумма кадра не сходится")
	}
	payload := body[4 : len(body)-endLen]
	if len(payload) == 0 {
		return cmd, nil, nil
	}
	switch s.version {
	case "3.3":
		payload = bytes.TrimPrefix(payload, versionHeader(s.version))
		plain, err := ecbDecrypt(s.key, payload)
		if err != nil {
			return 0, nil, drivers.Errorf(drivers.KindAuth, "не удалось расшифровать ответ: неверный local_key или версия протокола")
		}
		return cmd, plain, nil
	default:
		key := s.key
		if cmd == cmdSessKeyResp {
			key = s.realKey
		}
		plain, err := ecbDecrypt(key, payload)
		if err != nil {
			return 0, nil, drivers.Errorf(drivers.KindAuth, "не удалось расшифровать ответ: неверный local_key или версия протокола")
		}
		return cmd, bytes.TrimPrefix(plain, versionHeader(s.version)), nil
	}
}

func (s *tuyaSession) read6699(pfx []byte) (uint32, []byte, error) {
	hdr := make([]byte, 18)
	copy(hdr, pfx)
	if _, err := io.ReadFull(s.conn, hdr[4:]); err != nil {
		return 0, nil, err
	}
	cmd, n := binary.BigEndian.Uint32(hdr[10:]), binary.BigEndian.Uint32(hdr[14:])
	if n < 12+16 || n > maxTuyaFrame {
		return 0, nil, drivers.Errorf(drivers.KindProtocol, "повреждённый кадр")
	}
	body := make([]byte, n+4)
	if _, err := io.ReadFull(s.conn, body); err != nil {
		return 0, nil, err
	}
	gcm, _ := cipher.NewGCM(mustAES(s.key))
	plain, err := gcm.Open(nil, body[:12], body[12:n], hdr[4:])
	if err != nil {
		return 0, nil, drivers.Errorf(drivers.KindAuth, "не удалось расшифровать ответ: неверный local_key или версия протокола")
	}
	// Кадры от устройства начинаются с 4-байтового кода возврата.
	if len(plain) >= 4 {
		plain = plain[4:]
	}
	return cmd, bytes.TrimPrefix(plain, versionHeader(s.version)), nil
}

func (s *tuyaSession) nextSeq() uint32 {
	s.seq++
	return s.seq - 1
}

// dpsFromPayload достаёт точки данных из ответа: {"dps":{...}} или {"data":{"dps":{...}}}.
func dpsFromPayload(p []byte) (map[string]any, bool) {
	p = bytes.TrimSpace(p)
	if len(p) == 0 || p[0] != '{' {
		return nil, false
	}
	var msg struct {
		DPS  map[string]any `json:"dps"`
		Data struct {
			DPS map[string]any `json:"dps"`
		} `json:"data"`
	}
	dec := json.NewDecoder(bytes.NewReader(p))
	dec.UseNumber()
	if err := dec.Decode(&msg); err != nil {
		return nil, false
	}
	if msg.DPS != nil {
		return msg.DPS, true
	}
	return msg.Data.DPS, msg.Data.DPS != nil
}

func versionHeader(v string) []byte {
	return append([]byte(v), make([]byte, 12)...)
}

func hmacSHA256(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)
}

func mustAES(key []byte) cipher.Block {
	b, err := aes.NewCipher(key)
	if err != nil {
		panic(err) // длина ключа проверена заранее: 16 байт
	}
	return b
}

func ecbEncryptRaw(key, data []byte) []byte {
	b := mustAES(key)
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += aes.BlockSize {
		b.Encrypt(out[i:i+aes.BlockSize], data[i:i+aes.BlockSize])
	}
	return out
}

func ecbEncrypt(key, data []byte) []byte {
	pad := aes.BlockSize - len(data)%aes.BlockSize
	return ecbEncryptRaw(key, append(append([]byte{}, data...), bytes.Repeat([]byte{byte(pad)}, pad)...))
}

func ecbDecrypt(key, data []byte) ([]byte, error) {
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, errors.New("длина не кратна блоку")
	}
	b := mustAES(key)
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += aes.BlockSize {
		b.Decrypt(out[i:i+aes.BlockSize], data[i:i+aes.BlockSize])
	}
	pad := int(out[len(out)-1])
	if pad < 1 || pad > aes.BlockSize {
		return nil, errors.New("неверное дополнение")
	}
	return out[:len(out)-pad], nil
}
