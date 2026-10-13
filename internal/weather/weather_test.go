package weather

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNow(t *testing.T) {
	var geo, forecast int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/geo":
			geo++
			if r.URL.Query().Get("name") == "нигде" {
				_, _ = io.WriteString(w, `{}`)
				return
			}
			_, _ = io.WriteString(w, `{"results":[{"name":"Москва","latitude":55.75,"longitude":37.62}]}`)
		case "/forecast":
			forecast++
			if r.URL.Query().Get("latitude") != "55.7500" {
				t.Errorf("latitude = %q", r.URL.Query().Get("latitude"))
			}
			_, _ = io.WriteString(w, `{"current":{"temperature_2m":-3.4,"weather_code":71,"is_day":0}}`)
		}
	}))
	defer srv.Close()
	c := New(http.DefaultTransport)
	c.geocodeURL, c.forecastURL = srv.URL+"/geo", srv.URL+"/forecast"

	for range 2 {
		got, err := c.Now(context.Background(), " Москва ")
		if err != nil {
			t.Fatal(err)
		}
		if want := (Current{City: "Москва", Temperature: -3.4, Code: 71}); got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	}
	if geo != 1 || forecast != 1 {
		t.Fatalf("повторный вызов должен браться из кеша: geo=%d forecast=%d", geo, forecast)
	}

	if _, err := c.Now(context.Background(), "нигде"); !errors.Is(err, ErrCityNotFound) {
		t.Fatalf("err = %v, want ErrCityNotFound", err)
	}
}
