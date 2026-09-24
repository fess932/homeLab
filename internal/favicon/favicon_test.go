package favicon

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/fess932/homeLab/internal/netguard"
	"github.com/sergeymakinen/go-ico"
)

func pngBytes(t *testing.T, size int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestLinks(t *testing.T) {
	base, _ := url.Parse("https://me.example/app/")
	page := `<html><head>
		<link rel="icon" href="/favicon-16.png" sizes="16x16">
		<link rel="apple-touch-icon" href="touch.png">
		<link rel="icon" type="image/svg+xml" href="/favicon.svg">
		<link rel="stylesheet" href="/x.css">
		<link rel="icon" href="javascript:alert(1)">
	</head></html>`
	got := Links([]byte(page), base)
	want := []string{"https://me.example/favicon.svg", "https://me.example/app/touch.png", "https://me.example/favicon-16.png"}
	if len(got) != len(want) {
		t.Fatalf("Links: %v", got)
	}
	for i := range want {
		if got[i].String() != want[i] {
			t.Fatalf("Links[%d] = %s, ожидалось %s", i, got[i], want[i])
		}
	}
}

func TestFind(t *testing.T) {
	icon := pngBytes(t, 32)
	var icoBuf bytes.Buffer
	if err := ico.Encode(&icoBuf, image.NewRGBA(image.Rect(0, 0, 48, 48))); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	// Сайт с иконкой в HTML по нестандартному пути; первая объявленная — заглушка-HTML.
	mux.HandleFunc("/declared/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<link rel="icon" type="image/svg+xml" href="/fake.svg"><link rel="icon" href="/static/logo.png">`))
	})
	mux.HandleFunc("/fake.svg", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>not found</html>")) })
	mux.HandleFunc("/static/logo.png", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(icon) })
	// Сайт без объявлений: только /favicon.ico в формате ICO.
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(icoBuf.Bytes()) })
	mux.HandleFunc("/plain/", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html></html>")) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	f := New(http.DefaultTransport)
	isPNG := func(b []byte) error {
		if !bytes.HasPrefix(b, []byte("\x89PNG")) {
			return errors.New("не PNG")
		}
		return nil
	}
	var got []byte
	accept := func(b []byte) error {
		if err := isPNG(b); err != nil {
			return err
		}
		got = b
		return nil
	}
	if err := f.Find(context.Background(), srv.URL+"/declared/", accept); err != nil || !bytes.Equal(got, icon) {
		t.Fatalf("объявленная иконка: %v", err)
	}
	if err := f.Find(context.Background(), srv.URL+"/plain/", accept); err != nil {
		t.Fatalf("favicon.ico: %v", err)
	}
	if cfg, err := png.DecodeConfig(bytes.NewReader(got)); err != nil || cfg.Width != 48 {
		t.Fatalf("ICO не перекодирован в PNG: %v %+v", err, cfg)
	}
	if err := f.Find(context.Background(), "ftp://x/", accept); err == nil {
		t.Fatal("ftp:// должен отклоняться")
	}
}

// С транспортом netguard сервер не сходит на loopback: ни в панель, ни во внутренние порты.
func TestFindNetguard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(pngBytes(t, 16)) }))
	defer srv.Close()
	called := false
	err := New(netguard.Transport(nil)).Find(context.Background(), srv.URL, func([]byte) error { called = true; return nil })
	if err == nil || called {
		t.Fatalf("loopback должен блокироваться: %v", err)
	}
}
