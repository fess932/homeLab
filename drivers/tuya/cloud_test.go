package tuya

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Эталонные значения посчитаны оригинальным tuya-device-sharing-sdk (Python) для тех же входных данных.
func TestCloudCryptoMatchesSDK(t *testing.T) {
	rid, refresh := "11111111-2222-4333-8444-555555555555", "refresh-token-x"
	sum := md5.Sum([]byte(rid + refresh))
	hashKey := hex.EncodeToString(sum[:])
	if hashKey != "7a959443682a119475142c0e2f5bef77" {
		t.Fatalf("hash_key %s", hashKey)
	}
	secret := requestSecret(rid, hashKey)
	if string(secret) != "6e0d23e0ac11f294" {
		t.Fatalf("secret %s", secret)
	}
	headers := [][2]string{{"X-appKey", haClientID}, {"X-requestId", rid}, {"X-sid", ""}, {"X-time", "1790000000000"}, {"X-token", "access-x"}}
	if got := sign(hashKey, headers, "bm9uY2Vub25jZTEyY2lwaGVy"); got != "5646e27f1727796f5f76fc708371d69ba830a2df71c491c8b8d85835911469ed" {
		t.Fatalf("sign %s", got)
	}
	plain, err := gcmDecrypt("QUJDREVGYWJjZGVmOn8wABOfahdah+Xg65TiQ16TlHs8/Tin", secret)
	if err != nil || string(plain) != `{"ok":1}` {
		t.Fatalf("decrypt %q %v", plain, err)
	}
	// gcmEncrypt отдаёт base64(nonce)+base64(шифротекст): облако принимает именно так.
	enc := gcmEncrypt([]byte(`{"homeId":"1"}`), secret)
	if back, err := gcmDecrypt(encodedJoin(enc), secret); err != nil || string(back) != `{"homeId":"1"}` {
		t.Fatalf("формат encdata %q: %q %v", enc, back, err)
	}
}

// fakeCloud — облако Smart Life: вход по QR и API аккаунта с подписью и шифрованием.
type fakeCloud struct {
	t         *testing.T
	confirmed bool
	srv       *httptest.Server
}

func (f *fakeCloud) handler(w http.ResponseWriter, r *http.Request) {
	reply := func(result any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "t": time.Now().UnixMilli(), "result": result})
	}
	switch {
	case r.URL.Path == "/v1.0/m/life/home-assistant/qrcode/tokens" && r.Method == http.MethodPost:
		if r.URL.Query().Get("usercode") != "user42" || r.URL.Query().Get("clientid") != haClientID {
			f.t.Errorf("параметры QR: %s", r.URL.RawQuery)
		}
		reply(map[string]string{"qrcode": "qr-token-1"})
	case r.URL.Path == "/v1.0/m/life/home-assistant/qrcode/tokens/qr-token-1":
		if !f.confirmed {
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "code": 1010, "msg": "token invalid"})
			return
		}
		reply(map[string]any{"access_token": "acc", "refresh_token": "ref", "expire_time": 7200, "uid": "u1",
			"endpoint": f.srv.URL, "terminal_id": "term", "username": "me@example.com"})
	default:
		f.signedAPI(w, r)
	}
}

func (f *fakeCloud) signedAPI(w http.ResponseWriter, r *http.Request) {
	rid, token := r.Header.Get("X-requestId"), r.Header.Get("X-token")
	sum := md5.Sum([]byte(rid + "ref"))
	hashKey := hex.EncodeToString(sum[:])
	secret := requestSecret(rid, hashKey)
	var hdr [][2]string
	for _, h := range []string{"X-appKey", "X-requestId", "X-sid", "X-time", "X-token"} {
		hdr = append(hdr, [2]string{h, r.Header.Get(h)})
	}
	enc := r.URL.Query().Get("encdata")
	if token != "acc" || r.Header.Get("X-sign") != sign(hashKey, hdr, enc) {
		f.t.Errorf("подпись запроса %s не сходится", r.URL.Path)
		w.WriteHeader(http.StatusForbidden)
		return
	}
	params := map[string]string{}
	if enc != "" {
		// base64(nonce 12 байт) занимает 16 символов, дальше — base64 шифротекста.
		raw, err := gcmDecrypt(encodedJoin(enc), secret)
		if err != nil {
			f.t.Errorf("параметры не расшифрованы: %v", err)
		}
		_ = json.Unmarshal(raw, &params)
	}
	var result any
	switch {
	case r.URL.Path == "/v1.0/m/life/users/homes":
		result = []map[string]any{{"ownerId": 777, "name": "Дом"}}
	case r.URL.Path == "/v1.0/m/life/ha/home/devices" && params["homeId"] == "777",
		r.URL.Path == "/v1.0/m/life/ha/devices/detail" && params["devIds"] == "eb398c7f26966400abs3ju":
		result = []map[string]any{{"id": "eb398c7f26966400abs3ju", "name": "Датчик воздуха", "local_key": "0123456789abcdef", "product_id": "owmkja70doamcxkh", "online": true}}
	case r.URL.Path == "/v1.0/m/life/devices/eb398c7f26966400abs3ju/status":
		result = map[string]any{"productKey": "owmkja70doamcxkh", "dpStatusRelationDTOS": []map[string]any{
			{"dpId": 2, "statusCode": "temp_current", "valueType": "Integer", "valueDesc": `{"unit":"℃","scale":0}`, "supportLocal": true},
			{"dpId": 5, "statusCode": "ch2o_value", "valueType": "Integer", "valueDesc": `{"unit":"mg/m³","scale":3}`, "supportLocal": true},
		}}
	default:
		f.t.Errorf("неожиданный запрос %s %v", r.URL.Path, params)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	plain, _ := json.Marshal(result)
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "t": time.Now().UnixMilli(), "result": encryptResult(plain, secret)})
}

// encodedJoin превращает формат запроса base64(nonce)+base64(ct) в формат ответа base64(nonce+ct).
func encodedJoin(enc string) string {
	nonce, rest := enc[:16], enc[16:]
	a, _ := b64(nonce)
	b, _ := b64(rest)
	return b64enc(append(a, b...))
}

func TestCloudFlow(t *testing.T) {
	f := &fakeCloud{t: t}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.handler))
	defer f.srv.Close()
	d := &Driver{cloud: newCloud()}
	d.cloud.loginHost, d.cloud.client = f.srv.URL, f.srv.Client()
	ctx := context.Background()

	if _, err := d.StartLogin(ctx, map[string]string{"user_code": ""}); err == nil {
		t.Fatal("пустой код пользователя")
	}
	login, err := d.StartLogin(ctx, map[string]string{"user_code": "user42"})
	if err != nil || login.QR != "tuyaSmart--qrLogin?token=qr-token-1" {
		t.Fatalf("%+v %v", login, err)
	}
	if done, _, err := d.CheckLogin(ctx, login.ID); done || err != nil {
		t.Fatalf("до подтверждения: %v %v", done, err)
	}
	f.confirmed = true
	done, acc, err := d.CheckLogin(ctx, login.ID)
	if !done || err != nil || acc.Name != "Smart Life: me@example.com" {
		t.Fatalf("после подтверждения: %v %+v %v", done, acc, err)
	}

	list, _, err := d.ListDevices(ctx, acc.Data)
	if err != nil || len(list) != 1 {
		t.Fatalf("устройства: %+v %v", list, err)
	}
	c := list[0]
	if c.Name != "Датчик воздуха" || !c.HasKey || c.Ref != "eb398c7f26966400abs3ju" || strings.Contains(string(c.Config), "0123456789abcdef") {
		t.Fatalf("кандидат: %+v", c)
	}
	var cfg Config
	_ = json.Unmarshal(c.Config, &cfg)
	if s := cfg.Schema; len(s) != 2 || s[1].DP != "5" || s[1].Code != "ch2o_value" || s[1].Unit != "mg/m³" || s[1].Scale != 3 {
		t.Fatalf("схема: %+v", cfg.Schema)
	}

	adopted, _, err := d.Adopt(ctx, acc.Data, c.Ref)
	if err != nil || adopted.Key != "0123456789abcdef" {
		t.Fatalf("добавление: %+v %v", adopted, err)
	}
}

func b64(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }
func b64enc(b []byte) string       { return base64.StdEncoding.EncodeToString(b) }

func encryptResult(plain, key []byte) string {
	nonce := []byte("ABCDEFabcdef")
	blk, _ := aes.NewCipher(key)
	g, _ := cipher.NewGCM(blk)
	return b64enc(append(nonce, g.Seal(nil, nonce, plain, nil)...))
}
