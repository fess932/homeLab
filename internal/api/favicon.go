package api

import (
	"context"
	"encoding/json"
	"time"

	"github.com/fess932/homeLab/internal/model"
)

// resolveIcon заменяет icon "favicon" (команду «взять иконку с сайта») на загруженный
// файл asset:<id>. Иконку ищет сервер, а не браузер: HTML чужого сайта браузеру не
// прочитать, а файл с адреса панели виден и на https, и на публичной странице снаружи.
// Не нашлась — пустая иконка, интерфейс покажет глобус.
func (s *Server) resolveIcon(ctx context.Context, icon, pageURL string) string {
	if icon != model.IconFavicon {
		return icon
	}
	if s.Favicons == nil || s.Assets == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var id string
	err := s.Favicons.Find(ctx, pageURL, func(data []byte) error {
		a, err := s.Assets.SaveBytes(ctx, data)
		id = a.ID
		return err
	})
	if err != nil {
		s.Log.Info("иконка сайта не найдена", "url", pageURL, "err", err)
		return ""
	}
	return "asset:" + id
}

// resolvePageIcons подставляет иконки сайтов простым ссылкам страницы.
func (s *Server) resolvePageIcons(ctx context.Context, in *model.PageInput) {
	for i, w := range in.Widgets {
		if w.Type != model.WidgetLink {
			continue
		}
		var c model.LinkConfig
		if json.Unmarshal(w.Config, &c) != nil || c.Icon != model.IconFavicon || c.ServiceID != "" {
			continue
		}
		c.Icon = s.resolveIcon(ctx, c.Icon, c.URL)
		if raw, err := json.Marshal(c); err == nil {
			in.Widgets[i].Config = raw
		}
	}
}
