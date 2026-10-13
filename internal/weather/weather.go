// Package weather отдаёт текущую погоду в городе по данным Open-Meteo:
// название города превращается в координаты геокодером, ответы кешируются.
package weather

import (
	"context"
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
)

const (
	geocodeURL  = "https://geocoding-api.open-meteo.com/v1/search"
	forecastURL = "https://api.open-meteo.com/v1/forecast"
	timeout     = 10 * time.Second
	maxBody     = 1 << 20
	// freshFor — сколько живёт ответ: Open-Meteo обновляет текущую погоду раз в 15 минут.
	freshFor = 10 * time.Minute
	// retryAfter — пауза после ошибки, чтобы опрос виджета не бил по сервису каждые 15 секунд.
	retryAfter = time.Minute
	maxCities  = 100
)

var ErrCityNotFound = errors.New("город не найден")

// Current — погода сейчас. Code — код явления WMO, как его отдаёт Open-Meteo.
type Current struct {
	City        string  `json:"city"`
	Temperature float64 `json:"temperature"`
	Code        int     `json:"code"`
	IsDay       bool    `json:"is_day"`
}

type place struct {
	name     string
	lat, lon float64
}

type entry struct {
	place   *place
	current Current
	err     error
	until   time.Time
}

// Client ходит в сеть через HTTP: снаружи ему дают клиент с netguard.
type Client struct {
	HTTP *http.Client

	geocodeURL, forecastURL string
	mu                      sync.Mutex
	cities                  map[string]*entry
}

func New(tr http.RoundTripper) *Client {
	return &Client{
		HTTP:       &http.Client{Transport: tr, Timeout: timeout},
		geocodeURL: geocodeURL, forecastURL: forecastURL,
		cities: map[string]*entry{},
	}
}

// Now возвращает погоду в городе city; повторные вызовы в пределах freshFor берутся из кеша.
func (c *Client) Now(ctx context.Context, city string) (Current, error) {
	key := strings.ToLower(strings.TrimSpace(city))
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.cities[key]
	if e != nil && time.Now().Before(e.until) {
		return e.current, e.err
	}
	if e == nil {
		if len(c.cities) >= maxCities {
			clear(c.cities)
		}
		e = &entry{}
		c.cities[key] = e
	}
	e.current, e.err = c.fetch(ctx, e, key)
	e.until = time.Now().Add(freshFor)
	if e.err != nil {
		if ctx.Err() != nil {
			e.until = time.Time{}
		} else if !errors.Is(e.err, ErrCityNotFound) {
			e.until = time.Now().Add(retryAfter)
		}
	}
	return e.current, e.err
}

func (c *Client) fetch(ctx context.Context, e *entry, city string) (Current, error) {
	if e.place == nil {
		var geo struct {
			Results []struct {
				Name      string  `json:"name"`
				Latitude  float64 `json:"latitude"`
				Longitude float64 `json:"longitude"`
			} `json:"results"`
		}
		if err := c.get(ctx, c.geocodeURL, url.Values{"name": {city}, "count": {"1"}, "language": {"ru"}, "format": {"json"}}, &geo); err != nil {
			return Current{}, err
		}
		if len(geo.Results) == 0 {
			return Current{}, ErrCityNotFound
		}
		r := geo.Results[0]
		e.place = &place{name: r.Name, lat: r.Latitude, lon: r.Longitude}
	}
	var fc struct {
		Current struct {
			Temperature float64 `json:"temperature_2m"`
			Code        int     `json:"weather_code"`
			IsDay       int     `json:"is_day"`
		} `json:"current"`
	}
	q := url.Values{
		"latitude":  {strconv.FormatFloat(e.place.lat, 'f', 4, 64)},
		"longitude": {strconv.FormatFloat(e.place.lon, 'f', 4, 64)},
		"current":   {"temperature_2m,weather_code,is_day"},
	}
	if err := c.get(ctx, c.forecastURL, q, &fc); err != nil {
		return Current{}, err
	}
	return Current{City: e.place.name, Temperature: fc.Current.Temperature, Code: fc.Current.Code, IsDay: fc.Current.IsDay == 1}, nil
}

func (c *Client) get(ctx context.Context, base string, q url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("open-meteo: %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out)
}
