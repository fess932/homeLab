package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

const KeyVersion = 1

type Payload struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
	Key      string `json:"key,omitempty"`
	// Data — данные подключённого облачного аккаунта драйвера (токены и т. п.).
	Data json.RawMessage `json:"data,omitempty"`
}

type Box struct {
	aead cipher.AEAD
}

func LoadOrCreateKey(path string) (*Box, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		enc := base64.StdEncoding.EncodeToString(key) + "\n"
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, fmt.Errorf("создание ключа секретов: %w", err)
		}
		if _, err := f.WriteString(enc); err != nil {
			f.Close()
			return nil, err
		}
		if err := f.Sync(); err != nil {
			f.Close()
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
		raw = []byte(enc)
	} else if err != nil {
		return nil, fmt.Errorf("чтение ключа секретов: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("ключ секретов %s: ожидается 32 байта в base64", path)
	}
	return NewBox(key)
}

func NewBox(key []byte) (*Box, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

func (b *Box) Seal(id string, p Payload) ([]byte, error) {
	plain, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, plain, []byte(id)), nil
}

func (b *Box) Open(id string, sealed []byte) (Payload, error) {
	var p Payload
	n := b.aead.NonceSize()
	if len(sealed) < n {
		return p, errors.New("секрет повреждён")
	}
	plain, err := b.aead.Open(nil, sealed[:n], sealed[n:], []byte(id))
	if err != nil {
		return p, errors.New("секрет не расшифровывается: ключ не совпадает с базой")
	}
	return p, json.Unmarshal(plain, &p)
}
