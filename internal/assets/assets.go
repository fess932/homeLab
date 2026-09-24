package assets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/store"
	_ "golang.org/x/image/webp"
)

const (
	MaxSize      = 5 << 20
	MaxSVGSize   = 256 << 10
	MaxDimension = 8192
)

var (
	ErrTooLarge    = errors.New("файл больше 5 MiB")
	ErrUnsupported = errors.New("поддерживаются только PNG, JPEG, WebP и SVG")
)

var formats = map[string]struct{ mediaType, ext string }{
	"png":  {"image/png", ".png"},
	"jpeg": {"image/jpeg", ".jpg"},
	"webp": {"image/webp", ".webp"},
	"svg":  {"image/svg+xml", ".svg"},
}

type Service struct {
	Dir   string
	Store *store.Store
}

func (s *Service) Save(ctx context.Context, r io.Reader) (model.Asset, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxSize+1))
	if err != nil {
		return model.Asset{}, err
	}
	return s.SaveBytes(ctx, data)
}

func (s *Service) SaveBytes(ctx context.Context, data []byte) (model.Asset, error) {
	if len(data) > MaxSize {
		return model.Asset{}, ErrTooLarge
	}
	cfg, format, err := decodeConfig(data)
	f, ok := formats[format]
	if err != nil || !ok {
		return model.Asset{}, ErrUnsupported
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxDimension || cfg.Height > MaxDimension {
		return model.Asset{}, fmt.Errorf("%w: размер изображения до %dx%d", ErrUnsupported, MaxDimension, MaxDimension)
	}
	sum := sha256.Sum256(data)
	checksum := hex.EncodeToString(sum[:])
	if existing, err := s.Store.AssetByChecksum(ctx, checksum); err == nil {
		return existing, nil
	}
	id := model.NewID("ast")
	name := id + f.ext
	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		return model.Asset{}, err
	}
	tmp, err := os.CreateTemp(s.Dir, ".upload-*")
	if err != nil {
		return model.Asset{}, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return model.Asset{}, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return model.Asset{}, err
	}
	if err := tmp.Close(); err != nil {
		return model.Asset{}, err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(s.Dir, name)); err != nil {
		return model.Asset{}, err
	}
	a, err := s.Store.CreateAsset(ctx, model.Asset{ID: id, MediaType: f.mediaType, Size: int64(len(data)), Width: cfg.Width, Height: cfg.Height, Checksum: checksum, Path: name})
	if err != nil {
		_ = os.Remove(filepath.Join(s.Dir, name))
	}
	return a, err
}

func (s *Service) Open(a model.Asset) (*os.File, error) {
	return os.Open(filepath.Join(s.Dir, filepath.Base(a.Path)))
}

func (s *Service) Delete(ctx context.Context, id string) error {
	a, err := s.Store.GetAsset(ctx, id)
	if err != nil {
		return err
	}
	inUse, err := s.Store.AssetInUse(ctx, id)
	if err != nil {
		return err
	}
	if inUse {
		return model.ErrInUse
	}
	if err := s.Store.DeleteAsset(ctx, id); err != nil {
		return err
	}
	return os.Remove(filepath.Join(s.Dir, filepath.Base(a.Path)))
}

// decodeConfig определяет формат и размеры: растр — по сигнатуре и заголовку,
// SVG — по корневому элементу. SVG отдаётся с CSP sandbox, скрипты в нём не выполняются.
func decodeConfig(data []byte) (image.Config, string, error) {
	if cfg, ok := svgConfig(data); ok {
		return cfg, "svg", nil
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if f, ok := formats[format]; err == nil && ok && http.DetectContentType(data) != f.mediaType {
		return cfg, format, ErrUnsupported
	}
	return cfg, format, err
}

func svgConfig(data []byte) (image.Config, bool) {
	if len(data) > MaxSVGSize {
		return image.Config{}, false
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return image.Config{}, false
		}
		el, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if el.Name.Local != "svg" {
			return image.Config{}, false
		}
		// Размер для списка файлов: из width/height, иначе из viewBox, иначе условные 64.
		w, h := 64, 64
		attrs := map[string]string{}
		for _, a := range el.Attr {
			attrs[a.Name.Local] = a.Value
		}
		if vb := strings.Fields(strings.ReplaceAll(attrs["viewBox"], ",", " ")); len(vb) == 4 {
			w, h = dim(vb[2], w), dim(vb[3], h)
		}
		w, h = dim(attrs["width"], w), dim(attrs["height"], h)
		if !svgInert(el, dec) {
			return image.Config{}, false
		}
		return image.Config{Width: min(w, MaxDimension), Height: min(h, MaxDimension)}, true
	}
}

// svgInert проверяет весь документ: без скриптов, встроенного HTML, обработчиков событий
// и javascript:-ссылок. Это вторая линия защиты поверх CSP sandbox при выдаче.
func svgInert(root xml.StartElement, dec *xml.Decoder) bool {
	el := root
	for {
		switch strings.ToLower(el.Name.Local) {
		case "script", "foreignobject", "iframe", "embed", "object":
			return false
		}
		for _, a := range el.Attr {
			name, val := strings.ToLower(a.Name.Local), strings.ToLower(strings.TrimSpace(a.Value))
			if strings.HasPrefix(name, "on") || strings.Contains(val, "javascript:") {
				return false
			}
		}
		for {
			tok, err := dec.Token()
			if errors.Is(err, io.EOF) {
				return true
			}
			if err != nil {
				return false
			}
			if se, ok := tok.(xml.StartElement); ok {
				el = se
				break
			}
		}
	}
}

func dim(s string, fallback int) int {
	f, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "px"), 64)
	if err != nil || f < 1 {
		return fallback
	}
	return int(f)
}
