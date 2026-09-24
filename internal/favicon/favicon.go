// Package favicon находит иконку сайта: разбирает <link rel="icon"> страницы
// и скачивает лучшую из объявленных, а если их нет — /favicon.ico.
// ICO перекодируется в PNG, остальное отдаётся как есть на проверку в assets.
package favicon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sergeymakinen/go-ico"
	"golang.org/x/net/html"
)

const (
	maxPage   = 1 << 20
	maxIcon   = 1 << 20
	timeout   = 10 * time.Second
	redirects = 5
)

var ErrNotFound = errors.New("у сайта не найдена иконка")

// Finder ходит по сети через Client: снаружи ему дают клиент с netguard,
// чтобы запрос не мог попасть на loopback приложения или адреса метаданных облака.
type Finder struct {
	Client *http.Client
}

// New возвращает Finder поверх транспорта tr с ограничением числа редиректов.
func New(tr http.RoundTripper) *Finder {
	return &Finder{Client: &http.Client{
		Transport: tr,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= redirects {
				return errors.New("слишком много редиректов")
			}
			return checkScheme(req.URL)
		},
	}}
}

// Find перебирает иконки сайта pageURL от лучшей и отдаёт их accept, пока та не примет
// одну (вернёт nil): accept проверяет формат, так что заглушки вроде HTML-страниц отсеиваются.
func (f *Finder) Find(ctx context.Context, pageURL string, accept func([]byte) error) error {
	base, err := url.Parse(pageURL)
	if err != nil {
		return err
	}
	if err := checkScheme(base); err != nil {
		return err
	}
	var candidates []*url.URL
	if body, final, err := f.get(ctx, base, maxPage); err == nil {
		candidates = Links(body, final)
		base = final
	}
	candidates = append(candidates, base.ResolveReference(&url.URL{Path: "/favicon.ico"}))
	seen := map[string]bool{}
	for _, c := range candidates {
		if seen[c.String()] {
			continue
		}
		seen[c.String()] = true
		data, _, err := f.get(ctx, c, maxIcon)
		if err != nil {
			continue
		}
		if img, ok := normalize(data); ok && accept(img) == nil {
			return nil
		}
	}
	return ErrNotFound
}

func (f *Finder) get(ctx context.Context, u *url.URL, limit int64) ([]byte, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "HomeDeck (favicon)")
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(body)) > limit {
		return nil, nil, fmt.Errorf("%s: больше %d байт", u, limit)
	}
	return body, resp.Request.URL, nil
}

func checkScheme(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("адрес %q: нужен http:// или https://", u)
	}
	return nil
}

// Links возвращает иконки, объявленные в HTML, от лучшей к худшей:
// SVG, затем по заявленному размеру (apple-touch-icon без размера считается 180px).
func Links(page []byte, base *url.URL) []*url.URL {
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return nil
	}
	type icon struct {
		u     *url.URL
		score int
	}
	var icons []icon
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode {
			continue
		}
		attr := map[string]string{}
		for _, a := range n.Attr {
			attr[a.Key] = a.Val
		}
		if n.Data == "base" && attr["href"] != "" {
			if b, err := base.Parse(attr["href"]); err == nil {
				base = b
			}
			continue
		}
		if n.Data != "link" || attr["href"] == "" {
			continue
		}
		rel := strings.Fields(strings.ToLower(attr["rel"]))
		apple := slices.Contains(rel, "apple-touch-icon") || slices.Contains(rel, "apple-touch-icon-precomposed")
		if !apple && !slices.Contains(rel, "icon") {
			continue
		}
		u, err := base.Parse(attr["href"])
		if err != nil || checkScheme(u) != nil {
			continue
		}
		score := size(attr["sizes"])
		switch {
		case attr["type"] == "image/svg+xml" || strings.HasSuffix(strings.ToLower(u.Path), ".svg"):
			score = 10000
		case score == 0 && apple:
			score = 180
		case score == 0:
			score = 16
		}
		icons = append(icons, icon{u, score})
	}
	slices.SortStableFunc(icons, func(a, b icon) int { return b.score - a.score })
	out := make([]*url.URL, len(icons))
	for i, ic := range icons {
		out[i] = ic.u
	}
	return out
}

// size — наибольшая сторона из атрибута sizes («32x32 64x64», «any»).
func size(s string) int {
	best := 0
	for f := range strings.FieldsSeq(strings.ToLower(s)) {
		if f == "any" {
			return 1000
		}
		w, _, ok := strings.Cut(f, "x")
		if n, err := strconv.Atoi(w); ok && err == nil && n > best {
			best = n
		}
	}
	return best
}

// normalize перекодирует ICO в PNG; прочие форматы проверит assets.
func normalize(data []byte) ([]byte, bool) {
	if !bytes.HasPrefix(data, []byte{0, 0, 1, 0}) {
		return data, len(data) > 0
	}
	img, err := ico.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, false
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}
