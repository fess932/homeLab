// Package drivers — контракт драйверов устройств и их реестр.
//
// Драйвер знает, как разговаривать с устройствами одного вида: какие у них
// настройки, как их опросить и привести значения к общему виду (model.Reading).
// Ядро HomeDeck (internal/devices) про конкретные драйверы не знает: оно берёт
// драйвер из реестра по полю kind устройства.
//
// Кроме обязательного опроса драйвер может уметь больше — это отдельные
// интерфейсы, которые ядро проверяет приведением типа:
//   - Discoverer: поиск устройств в локальной сети;
//   - AccountProvider: подключение облачного аккаунта (например, вход по QR)
//     и получение из него списка устройств вместе с ключами.
//
// Новый драйвер — пакет в drivers/<имя>, который регистрирует себя в init()
// и подключается в drivers/all.
package drivers

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/secrets"
)

// Info описывает драйвер для API и UI.
type Info struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// SecretKinds — какие учётные данные подходят устройству: model.SecretKey,
	// "basic", "bearer". Пустой список — устройство без учётных данных.
	SecretKinds []string `json:"secret_kinds"`
	// SecretRequired — без учётных данных включённое устройство не опросить.
	SecretRequired bool `json:"secret_required"`
	Discover       bool `json:"discover"`
	Accounts       bool `json:"accounts"`
}

// Target — всё, что нужно драйверу для одного опроса.
type Target struct {
	DeviceID string
	Address  string
	Config   json.RawMessage
	Secret   secrets.Payload
	// Session — что драйвер запомнил после прошлого успешного опроса (например,
	// версию протокола); пусто после ошибки.
	Session string
}

type Result struct {
	Readings []model.Reading
	Session  string
	// Protocol — короткая подпись для UI: версия протокола, модель и т. п.
	Protocol string
}

type Driver interface {
	Info() Info
	// Normalize проставляет значения по умолчанию и проверяет адрес и настройки.
	// Ошибки — *model.ValidationError с полями address или config.<поле>.
	Normalize(address string, config json.RawMessage) (string, json.RawMessage, error)
	Poll(ctx context.Context, t Target) (Result, error)
}

// Candidate — устройство, которое драйвер нашёл в сети или в аккаунте и может добавить.
type Candidate struct {
	Name      string          `json:"name"`
	Address   string          `json:"address"`
	Config    json.RawMessage `json:"config"`
	ProductID string          `json:"product_id,omitempty"`
	// Ref — как сослаться на устройство в аккаунте при добавлении; ключ из облака
	// в браузер не отдаётся, сервер берёт его сам.
	Ref       string `json:"ref,omitempty"`
	AccountID string `json:"account_id,omitempty"`
	// HasKey — ключ известен (из аккаунта), устройство добавится без ввода ключа.
	HasKey bool `json:"has_key"`
	// InNetwork — устройство ответило в локальной сети при поиске.
	InNetwork bool   `json:"in_network"`
	Online    *bool  `json:"online,omitempty"`
	Note      string `json:"note,omitempty"`
}

type DiscoverOptions struct {
	// Subnet — где искать; пусто — подсети сетевых интерфейсов HomeDeck.
	Subnet string
	Wait   time.Duration
}

type DiscoverResult struct {
	Candidates []Candidate `json:"candidates"`
	Subnets    []string    `json:"subnets"`
	Warnings   []string    `json:"warnings"`
}

type Discoverer interface {
	Discover(ctx context.Context, opts DiscoverOptions) (DiscoverResult, error)
}

// Login — начатый вход в облачный аккаунт: QR-код, который нужно отсканировать в приложении.
type Login struct {
	ID      string    `json:"id"`
	QR      string    `json:"qr"`
	Hint    string    `json:"hint"`
	Expires time.Time `json:"expires"`
}

// Account — подключённый аккаунт: данные для секрета и подпись для UI.
type Account struct {
	Name string
	Data json.RawMessage
}

// Adopted — всё, чтобы сохранить устройство из аккаунта: настройки и его ключ.
type Adopted struct {
	Candidate Candidate
	Key       string
}

type AccountProvider interface {
	// StartLogin начинает вход; params — то, что пользователь ввёл (например, код пользователя).
	StartLogin(ctx context.Context, params map[string]string) (Login, error)
	// CheckLogin проверяет, подтверждён ли вход; done=false — ждать дальше.
	CheckLogin(ctx context.Context, loginID string) (done bool, acc Account, err error)
	// ListDevices возвращает устройства аккаунта и, если токены обновились, новые данные аккаунта.
	ListDevices(ctx context.Context, account json.RawMessage) ([]Candidate, json.RawMessage, error)
	// Adopt получает из аккаунта ключ и настройки устройства по Candidate.Ref.
	Adopt(ctx context.Context, account json.RawMessage, ref string) (Adopted, json.RawMessage, error)
}

// Виды ошибок драйвера для UI; сетевые ошибки классифицирует internal/probe.
const (
	KindAuth     = "auth"
	KindProtocol = "protocol"
	KindParse    = "parse"
	KindCloud    = "cloud"
)

// Error — устройство или облако ответили, но не так, как ожидалось.
type Error struct {
	Kind string
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

func Errorf(kind, msg string) error { return &Error{Kind: kind, Msg: msg} }

// Identifier умеет назвать устройство так же, как Candidate.Ref: по нему API
// отмечает найденные устройства, которые уже добавлены.
type Identifier interface {
	Identity(config json.RawMessage) string
}

// AccountKind — вид секрета с данными облачного аккаунта драйвера.
func AccountKind(driverKind string) string { return "account:" + driverKind }

// ErrUnknown — устройство с неизвестным драйвером (например, из экспорта более новой версии).
var ErrUnknown = errors.New("неизвестный тип устройства")

var (
	mu       sync.RWMutex
	registry = map[string]Driver{}
)

// Register добавляет драйвер в реестр; вызывается из init() пакета драйвера.
func Register(d Driver) {
	mu.Lock()
	defer mu.Unlock()
	kind := d.Info().Kind
	if _, dup := registry[kind]; dup {
		panic("драйвер " + kind + " зарегистрирован дважды")
	}
	registry[kind] = d
}

func Get(kind string) (Driver, bool) {
	mu.RLock()
	defer mu.RUnlock()
	d, ok := registry[kind]
	return d, ok
}

func All() []Driver {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Driver, 0, len(registry))
	for _, d := range registry {
		out = append(out, d)
	}
	slices.SortFunc(out, func(a, b Driver) int {
		if a.Info().Title < b.Info().Title {
			return -1
		}
		return 1
	})
	return out
}

// Validate нормализует адрес и настройки устройства драйвером его вида.
func Validate(in *model.DeviceInput) error {
	d, ok := Get(in.Kind)
	if !ok {
		return model.Invalid("kind", "неизвестный тип устройства")
	}
	addr, cfg, err := d.Normalize(in.Address, in.Config)
	if err != nil {
		return err
	}
	in.Address, in.Config = addr, cfg
	info := d.Info()
	if in.SecretID == nil && info.SecretRequired && *in.Enabled {
		return model.Invalid("secret_id", "нужны учётные данные устройства")
	}
	return nil
}
