package assets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/store"
	_ "golang.org/x/image/webp"
)

const (
	MaxSize      = 5 << 20
	MaxDimension = 8192
)

var (
	ErrTooLarge    = errors.New("файл больше 5 MiB")
	ErrUnsupported = errors.New("поддерживаются только PNG, JPEG и WebP")
)

var formats = map[string]struct{ mediaType, ext string }{
	"png":  {"image/png", ".png"},
	"jpeg": {"image/jpeg", ".jpg"},
	"webp": {"image/webp", ".webp"},
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
	sniffed := http.DetectContentType(data)
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	f, ok := formats[format]
	if err != nil || !ok || sniffed != f.mediaType {
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
