package auth

import (
	"strings"
	"testing"
)

func TestPassword(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("формат хеша: %s", h)
	}
	h2, _ := HashPassword("correct horse")
	if h == h2 {
		t.Fatal("соль должна быть случайной")
	}
	if ok, err := VerifyPassword(h, "correct horse"); !ok || err != nil {
		t.Fatalf("верный пароль: %v %v", ok, err)
	}
	if ok, _ := VerifyPassword(h, "wrong horse"); ok {
		t.Fatal("неверный пароль принят")
	}
	for _, bad := range []string{"", "$bcrypt$x", "$argon2id$v=18$m=1,t=1,p=1$AA$AA", "$argon2id$v=19$m=1,t=1,p=1$!!$AA"} {
		if ok, err := VerifyPassword(bad, "x"); ok || err == nil {
			t.Errorf("%q: должна быть ошибка", bad)
		}
	}
}

func TestTokens(t *testing.T) {
	a, b := RandomToken(), RandomToken()
	if a == b || len(a) != 43 {
		t.Fatalf("токены: %q %q", a, b)
	}
	if !EqualTokens(a, a) || EqualTokens(a, b) || EqualTokens("", a) {
		t.Fatal("EqualTokens")
	}
	if string(HashToken(a)) == a || len(HashToken(a)) != 32 {
		t.Fatal("HashToken")
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter(1, 3)
	for i := range 3 {
		if !l.Allow("ip1") {
			t.Fatalf("попытка %d в пределах burst отклонена", i)
		}
	}
	if l.Allow("ip1") {
		t.Fatal("превышение burst должно отклоняться")
	}
	// Ключи независимы: один клиент не блокирует другого.
	if !l.Allow("ip2") {
		t.Fatal("другой ключ")
	}
}
