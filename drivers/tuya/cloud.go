package tuya

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fess932/homeLab/drivers"
	"github.com/fess932/homeLab/internal/netguard"
)

// Вход через Smart Life по QR-коду и чтение устройств аккаунта — тот же API, что у
// интеграции Tuya в Home Assistant (tuya-device-sharing-sdk). Своего идентификатора
// клиента у HomeDeck нет, поэтому используется публичный идентификатор Home Assistant.
// Это неофициальное использование: Tuya может его ограничить. Облако нужно только
// при подключении аккаунта и добавлении устройств, опрос дальше идёт локально.
const (
	loginHost  = "https://apigw.iotbing.com"
	haClientID = "HA_3y9q4ak7g4ephrvke"
	haSchema   = "haauthorize"
	loginTTL   = 5 * time.Minute
	qrPrefix   = "tuyaSmart--qrLogin?token="
	maxBody    = 4 << 20
)

type pendingLogin struct {
	userCode, token string
	expires         time.Time
}

type cloud struct {
	loginHost string
	client    *http.Client
	mu        sync.Mutex
	logins    map[string]*pendingLogin
}

func newCloud() *cloud {
	return &cloud{
		loginHost: loginHost,
		client:    &http.Client{Transport: netguard.Transport(nil), CheckRedirect: netguard.NoRedirects, Timeout: 20 * time.Second},
		logins:    map[string]*pendingLogin{},
	}
}

// account — данные подключённого аккаунта; хранятся зашифрованным секретом.
type account struct {
	UserCode     string `json:"user_code"`
	Endpoint     string `json:"endpoint"`
	UID          string `json:"uid"`
	Username     string `json:"username"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"` // unix, мс
	TerminalID   string `json:"terminal_id"`
}

func (d *Driver) StartLogin(ctx context.Context, params map[string]string) (drivers.Login, error) {
	code := strings.TrimSpace(params["user_code"])
	if code == "" || len(code) > 64 || strings.ContainsAny(code, "/?&# ") {
		return drivers.Login{}, drivers.Errorf(drivers.KindAuth, "нужен код пользователя из приложения Smart Life: Я → Настройки → Аккаунт и безопасность → Код пользователя")
	}
	var res struct {
		QRCode string `json:"qrcode"`
	}
	q := url.Values{"clientid": {haClientID}, "usercode": {code}, "schema": {haSchema}}
	if err := d.cloud.plain(ctx, http.MethodPost, d.cloud.loginHost+"/v1.0/m/life/home-assistant/qrcode/tokens?"+q.Encode(), &res, nil); err != nil {
		return drivers.Login{}, err
	}
	if res.QRCode == "" {
		return drivers.Login{}, drivers.Errorf(drivers.KindCloud, "облако Tuya не выдало QR-код")
	}
	id := randomString(24)
	exp := time.Now().Add(loginTTL)
	d.cloud.mu.Lock()
	for k, l := range d.cloud.logins {
		if time.Now().After(l.expires) {
			delete(d.cloud.logins, k)
		}
	}
	d.cloud.logins[id] = &pendingLogin{userCode: code, token: res.QRCode, expires: exp}
	d.cloud.mu.Unlock()
	return drivers.Login{ID: id, QR: qrPrefix + res.QRCode, Hint: "Отсканируйте QR-код в приложении Smart Life или Tuya Smart и подтвердите вход", Expires: exp}, nil
}

func (d *Driver) CheckLogin(ctx context.Context, id string) (bool, drivers.Account, error) {
	d.cloud.mu.Lock()
	l, ok := d.cloud.logins[id]
	d.cloud.mu.Unlock()
	if !ok || time.Now().After(l.expires) {
		return false, drivers.Account{}, drivers.Errorf(drivers.KindAuth, "QR-код устарел, начните вход заново")
	}
	var res struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpireTime   int64  `json:"expire_time"`
		UID          string `json:"uid"`
		Endpoint     string `json:"endpoint"`
		TerminalID   string `json:"terminal_id"`
		Username     string `json:"username"`
	}
	var t int64
	q := url.Values{"clientid": {haClientID}, "usercode": {l.userCode}}
	err := d.cloud.plain(ctx, http.MethodGet, d.cloud.loginHost+"/v1.0/m/life/home-assistant/qrcode/tokens/"+url.PathEscape(l.token)+"?"+q.Encode(), &res, &t)
	if err != nil {
		// Пока вход не подтверждён в приложении, облако отвечает неуспехом — это не ошибка.
		if e := (*drivers.Error)(nil); errors.As(err, &e) && e.Kind == drivers.KindCloud {
			return false, drivers.Account{}, nil
		}
		return false, drivers.Account{}, err
	}
	if res.AccessToken == "" || !strings.HasPrefix(res.Endpoint, "https://") {
		return false, drivers.Account{}, nil
	}
	d.cloud.mu.Lock()
	delete(d.cloud.logins, id)
	d.cloud.mu.Unlock()
	if t == 0 {
		t = time.Now().UnixMilli()
	}
	acc := account{UserCode: l.userCode, Endpoint: res.Endpoint, UID: res.UID, Username: res.Username, AccessToken: res.AccessToken,
		RefreshToken: res.RefreshToken, ExpiresAt: t + res.ExpireTime*1000, TerminalID: res.TerminalID}
	name := res.Username
	if name == "" {
		name = res.UID
	}
	data, _ := json.Marshal(acc)
	return true, drivers.Account{Name: "Smart Life: " + name, Data: data}, nil
}

type cloudDevice struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	LocalKey    string `json:"local_key"`
	ProductID   string `json:"product_id"`
	ProductName string `json:"product_name"`
	Online      bool   `json:"online"`
	Sub         bool   `json:"sub"`
}

func (d *Driver) ListDevices(ctx context.Context, raw json.RawMessage) ([]drivers.Candidate, json.RawMessage, error) {
	acc, err := parseAccount(raw)
	if err != nil {
		return nil, nil, err
	}
	var homes []struct {
		OwnerID json.Number `json:"ownerId"`
		Name    string      `json:"name"`
	}
	if err := d.cloud.call(ctx, &acc, http.MethodGet, "/v1.0/m/life/users/homes", nil, &homes); err != nil {
		return nil, nil, err
	}
	out := []drivers.Candidate{}
	for _, h := range homes {
		var list []cloudDevice
		if err := d.cloud.call(ctx, &acc, http.MethodGet, "/v1.0/m/life/ha/home/devices", map[string]any{"homeId": h.OwnerID.String()}, &list); err != nil {
			return nil, nil, err
		}
		for _, cd := range list {
			c, err := d.candidate(ctx, &acc, cd)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, c)
		}
	}
	return out, accountJSON(acc), nil
}

func (d *Driver) Adopt(ctx context.Context, raw json.RawMessage, ref string) (drivers.Adopted, json.RawMessage, error) {
	acc, err := parseAccount(raw)
	if err != nil {
		return drivers.Adopted{}, nil, err
	}
	if !idRe.MatchString(ref) {
		return drivers.Adopted{}, nil, drivers.Errorf(drivers.KindCloud, "неверный id устройства")
	}
	var list []cloudDevice
	if err := d.cloud.call(ctx, &acc, http.MethodGet, "/v1.0/m/life/ha/devices/detail", map[string]any{"devIds": ref}, &list); err != nil {
		return drivers.Adopted{}, nil, err
	}
	if len(list) == 0 {
		return drivers.Adopted{}, nil, drivers.Errorf(drivers.KindCloud, "устройство не найдено в аккаунте")
	}
	c, err := d.candidate(ctx, &acc, list[0])
	if err != nil {
		return drivers.Adopted{}, nil, err
	}
	if list[0].LocalKey == "" {
		return drivers.Adopted{}, nil, drivers.Errorf(drivers.KindCloud, "облако не отдало ключ устройства")
	}
	return drivers.Adopted{Candidate: c, Key: list[0].LocalKey}, accountJSON(acc), nil
}

// candidate дополняет устройство аккаунта описанием точек данных для локального протокола.
func (d *Driver) candidate(ctx context.Context, acc *account, cd cloudDevice) (drivers.Candidate, error) {
	var st struct {
		ProductKey string `json:"productKey"`
		Relations  []struct {
			DPID         int    `json:"dpId"`
			StatusCode   string `json:"statusCode"`
			ValueType    string `json:"valueType"`
			ValueDesc    string `json:"valueDesc"`
			SupportLocal bool   `json:"supportLocal"`
		} `json:"dpStatusRelationDTOS"`
	}
	if err := d.cloud.call(ctx, acc, http.MethodGet, "/v1.0/m/life/devices/"+url.PathEscape(cd.ID)+"/status", nil, &st); err != nil {
		return drivers.Candidate{}, err
	}
	schema := []DP{}
	local := true
	for _, r := range st.Relations {
		local = local && r.SupportLocal
		schema = append(schema, describe(strconv.Itoa(r.DPID), r.StatusCode, r.ValueType, r.ValueDesc))
	}
	cfg, _ := json.Marshal(Config{DeviceID: cd.ID, Version: "auto", ProductID: cd.ProductID, Schema: schema})
	name := cd.Name
	if name == "" {
		name = cd.ProductName
	}
	c := drivers.Candidate{Name: name, Config: cfg, ProductID: cd.ProductID, Ref: cd.ID, HasKey: cd.LocalKey != "", Online: new(cd.Online)}
	switch {
	case cd.Sub:
		c.Note = "устройство за шлюзом: напрямую по локальному протоколу не опрашивается"
	case !local:
		c.Note = "облако сообщает, что устройство не поддерживает локальный протокол"
	}
	return c, nil
}

// describe переводит описание значения из облака ({"unit":"℃","scale":0} или {"range":[...]}) в DP.
func describe(dp, code, typ, desc string) DP {
	var d struct {
		Unit  string   `json:"unit"`
		Scale int      `json:"scale"`
		Range []string `json:"range"`
	}
	_ = json.Unmarshal([]byte(desc), &d)
	return DP{DP: dp, Code: code, Type: typ, Unit: d.Unit, Scale: d.Scale, Range: d.Range}
}

func parseAccount(raw json.RawMessage) (account, error) {
	var a account
	if err := json.Unmarshal(raw, &a); err != nil || a.RefreshToken == "" || !strings.HasPrefix(a.Endpoint, "https://") {
		return a, drivers.Errorf(drivers.KindAuth, "данные аккаунта повреждены: подключите аккаунт заново")
	}
	return a, nil
}

func accountJSON(a account) json.RawMessage {
	b, _ := json.Marshal(a)
	return b
}

type envelope struct {
	Success bool            `json:"success"`
	Code    json.RawMessage `json:"code"`
	Msg     string          `json:"msg"`
	T       int64           `json:"t"`
	Result  json.RawMessage `json:"result"`
}

// plain — запрос входа по QR: без подписи и шифрования.
func (c *cloud) plain(ctx context.Context, method, u string, out any, t *int64) error {
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return err
	}
	env, err := c.do(req)
	if err != nil {
		return err
	}
	if t != nil {
		*t = env.T
	}
	return json.Unmarshal(env.Result, out)
}

func (c *cloud) do(req *http.Request) (envelope, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return envelope{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return envelope{}, err
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return envelope{}, drivers.Errorf(drivers.KindCloud, fmt.Sprintf("облако Tuya ответило HTTP %d не в JSON", resp.StatusCode))
	}
	if !env.Success {
		msg := env.Msg
		if msg == "" {
			msg = "запрос отклонён"
		}
		return env, drivers.Errorf(drivers.KindCloud, "облако Tuya: "+msg)
	}
	return env, nil
}

// call — подписанный запрос к API аккаунта. Параметры и ответ шифруются AES-GCM
// ключом, выведенным из id запроса и refresh-токена; подпись — HMAC-SHA256 заголовков.
func (c *cloud) call(ctx context.Context, acc *account, method, path string, params map[string]any, out any) error {
	if err := c.refresh(ctx, acc); err != nil {
		return err
	}
	return c.signed(ctx, acc, method, path, params, out)
}

func (c *cloud) refresh(ctx context.Context, acc *account) error {
	if time.Now().UnixMilli() < acc.ExpiresAt-60_000 {
		return nil
	}
	var res struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpireTime   int64  `json:"expireTime"`
		UID          string `json:"uid"`
	}
	if err := c.signed(ctx, acc, http.MethodGet, "/v1.0/m/token/"+url.PathEscape(acc.RefreshToken), nil, &res); err != nil {
		return drivers.Errorf(drivers.KindAuth, "не удалось обновить вход в аккаунт, подключите его заново: "+err.Error())
	}
	acc.AccessToken, acc.RefreshToken = res.AccessToken, res.RefreshToken
	acc.ExpiresAt = time.Now().UnixMilli() + res.ExpireTime*1000
	return nil
}

func (c *cloud) signed(ctx context.Context, acc *account, method, path string, params map[string]any, out any) error {
	rid := uuid4()
	sum := md5.Sum([]byte(rid + acc.RefreshToken))
	hashKey := hex.EncodeToString(sum[:])
	secret := requestSecret(rid, hashKey)

	u := acc.Endpoint + path
	queryEnc := ""
	if len(params) > 0 {
		raw, _ := json.Marshal(params)
		queryEnc = gcmEncrypt(raw, secret)
		u += "?" + url.Values{"encdata": {queryEnc}}.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return err
	}
	headers := [][2]string{
		{"X-appKey", haClientID},
		{"X-requestId", rid},
		{"X-sid", ""},
		{"X-time", strconv.FormatInt(time.Now().UnixMilli(), 10)},
		{"X-token", acc.AccessToken},
	}
	for _, h := range headers {
		req.Header.Set(h[0], h[1])
	}
	req.Header.Set("X-sign", sign(hashKey, headers, queryEnc))

	env, err := c.do(req)
	if err != nil {
		return err
	}
	var enc string
	if err := json.Unmarshal(env.Result, &enc); err != nil {
		return json.Unmarshal(env.Result, out)
	}
	plain, err := gcmDecrypt(enc, secret)
	if err != nil {
		return drivers.Errorf(drivers.KindCloud, "не удалось расшифровать ответ облака Tuya")
	}
	return json.Unmarshal(plain, out)
}

// sign — HMAC-SHA256 ключом hashKey от непустых заголовков «имя=значение» через || и зашифрованных параметров.
func sign(hashKey string, headers [][2]string, queryEnc string) string {
	var parts []string
	for _, h := range headers {
		if h[1] != "" {
			parts = append(parts, h[0]+"="+h[1])
		}
	}
	m := hmac.New(sha256.New, []byte(hashKey))
	m.Write([]byte(strings.Join(parts, "||") + queryEnc))
	return hex.EncodeToString(m.Sum(nil))
}

// requestSecret — ключ AES-128 запроса: первые 16 hex-символов HMAC-SHA256(rid, hashKey).
func requestSecret(rid, hashKey string) []byte {
	m := hmac.New(sha256.New, []byte(rid))
	m.Write([]byte(hashKey))
	return []byte(hex.EncodeToString(m.Sum(nil))[:16])
}

// gcmEncrypt возвращает base64(nonce) + base64(шифротекст с тегом), как ожидает облако.
func gcmEncrypt(plain, key []byte) string {
	nonce := []byte(randomString(12))
	b, _ := aes.NewCipher(key)
	g, _ := cipher.NewGCM(b)
	return base64.StdEncoding.EncodeToString(nonce) + base64.StdEncoding.EncodeToString(g.Seal(nil, nonce, plain, nil))
}

func gcmDecrypt(enc string, key []byte) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil || len(raw) < 12+16 {
		return nil, fmt.Errorf("короткий ответ")
	}
	b, _ := aes.NewCipher(key)
	g, _ := cipher.NewGCM(b)
	return g.Open(nil, raw[:12], raw[12:], nil)
}

const nonceAlphabet = "ABCDEFGHJKMNPQRSTWXYZabcdefhijkmnprstwxyz2345678"

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = nonceAlphabet[int(b[i])%len(nonceAlphabet)]
	}
	return string(b)
}

func uuid4() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
