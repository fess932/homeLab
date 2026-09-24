package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fess932/homeLab/internal/model"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func TestMigrations(t *testing.T) {
	ctx := context.Background()
	s, path := open(t)
	var v int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil || v != SchemaVersion() {
		t.Fatalf("user_version %d, ожидалось %d (%v)", v, SchemaVersion(), err)
	}
	st, err := s.GetSettings(ctx)
	if err != nil || st.Title != "HomeDeck" {
		t.Fatalf("начальные настройки: %+v %v", st, err)
	}
	s.Close()

	// Повторное открытие не применяет миграции заново и не делает копию.
	s2, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".pre-migration-1"); !os.IsNotExist(err) {
		t.Error("копия перед миграцией не нужна, когда схема актуальна")
	}
	// Схема новее поддерживаемой: запуск старого образа на новой базе должен остановиться, а не сломать данные.
	if _, err := s2.db.ExecContext(ctx, "PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	s2.Close()
	if _, err := Open(ctx, path); err == nil || !strings.Contains(err.Error(), "новее") {
		t.Fatalf("ожидался отказ для схемы новее: %v", err)
	}
}

func TestCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	if err := os.WriteFile(path, []byte(strings.Repeat("это не sqlite ", 1000)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(context.Background(), path)
	if _, ok := errors.AsType[*CorruptError](err); !ok {
		t.Fatalf("ожидалась CorruptError, получено %v", err)
	}
}

func pageInput(slug string) model.PageInput {
	p := model.PageInput{
		Title:  "Дом",
		Slug:   slug,
		Groups: []model.Group{{ID: "new_g", Title: "Медиа"}},
		Widgets: []model.Widget{{
			ID: "new_w", GroupID: "new_g", Type: model.WidgetNote, Config: json.RawMessage(`{"markdown":"x"}`),
			Layout: model.Layout{LG: &model.Rect{W: 3, H: 1}},
		}},
	}
	p.Normalize()
	return p
}

func TestPagesAtomicSave(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	p, err := s.CreatePage(ctx, pageInput("home"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.Groups[0].ID, "grp_") || !strings.HasPrefix(p.Widgets[0].ID, "wgt_") || p.Widgets[0].GroupID != p.Groups[0].ID {
		t.Fatalf("new_* должны заменяться серверными id со связью виджет→группа: %+v", p)
	}
	st, _ := s.GetSettings(ctx)
	if st.StartPageID == nil || *st.StartPageID != p.ID {
		t.Fatal("первая страница должна стать стартовой")
	}

	// Повторное сохранение сохраняет существующие id и добавляет новые.
	in := p.PageInput
	in.Widgets = append(in.Widgets, model.Widget{ID: "new_x", GroupID: p.Groups[0].ID, Type: model.WidgetClock, Config: json.RawMessage(`{}`)})
	p2, err := s.UpdatePage(ctx, p.ID, p.Revision, in)
	if err != nil {
		t.Fatal(err)
	}
	if p2.Revision != p.Revision+1 || p2.Widgets[0].ID != p.Widgets[0].ID || p2.Groups[0].ID != p.Groups[0].ID || !strings.HasPrefix(p2.Widgets[1].ID, "wgt_") {
		t.Fatalf("id после повторного сохранения: %+v", p2)
	}

	// Устаревшая ревизия — конфликт, содержимое не меняется.
	in.Title = "Другое"
	if _, err := s.UpdatePage(ctx, p.ID, p.Revision, in); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("ожидался ErrConflict: %v", err)
	}
	cur, _ := s.GetPage(ctx, "home")
	if cur.Title != "Дом" || len(cur.Widgets) != 2 {
		t.Fatalf("конфликт не должен менять страницу: %+v", cur)
	}

	// id виджета с другой страницы нельзя «украсть»: он получит новый id.
	other, err := s.CreatePage(ctx, pageInput("lab"))
	if err != nil {
		t.Fatal(err)
	}
	oin := other.PageInput
	oin.Widgets[0].ID = p.Widgets[0].ID
	other2, err := s.UpdatePage(ctx, other.ID, other.Revision, oin)
	if err != nil {
		t.Fatal(err)
	}
	if other2.Widgets[0].ID == p.Widgets[0].ID {
		t.Fatal("чужой id виджета не должен переиспользоваться")
	}

	if _, err := s.CreatePage(ctx, pageInput("home")); err == nil {
		t.Fatal("повторный slug должен отклоняться")
	}

	// Удаление стартовой и публичной страницы сбрасывает ссылки в настройках.
	st, _ = s.GetSettings(ctx)
	st.PublicPageID = &p.ID
	if _, err := s.UpdateSettings(ctx, st.Revision, st); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePage(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	st, _ = s.GetSettings(ctx)
	if st.PublicPageID != nil || st.StartPageID == nil || *st.StartPageID != other.ID {
		t.Fatalf("после удаления: start=%v public=%v", st.StartPageID, st.PublicPageID)
	}
	if err := s.DeletePage(ctx, p.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("повторное удаление: %v", err)
	}
	if _, _, err := s.Widget(ctx, p.Widgets[0].ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("виджеты удаляются каскадно")
	}
}

func service(t *testing.T, s *Store, name string) model.Service {
	t.Helper()
	in := model.ServiceInput{Name: name, URL: "http://" + name}
	in.Normalize()
	sv, err := s.CreateService(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return sv
}

func checkFor(id string) model.CheckInput {
	c := model.CheckInput{ServiceID: id, Kind: "http", Target: "http://nas/"}
	c.Normalize()
	return c
}

func TestServiceChecks(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	sv := service(t, s, "nas")
	c, err := s.CreateCheck(ctx, checkFor(sv.ID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCheck(ctx, checkFor(sv.ID)); err == nil {
		t.Fatal("у сервиса одна проверка")
	}
	if _, err := s.CreateCheck(ctx, checkFor("svc_missing")); err == nil {
		t.Fatal("проверка для несуществующего сервиса")
	}
	got, _ := s.GetService(ctx, sv.ID)
	if got.CheckID == nil || *got.CheckID != c.ID {
		t.Fatal("check_id сервиса")
	}
	if err := s.DeleteService(ctx, sv.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetCheck(ctx, c.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("проверка удаляется вместе с сервисом")
	}
}

func source(t *testing.T, s *Store, secretID *string) model.Source {
	t.Helper()
	in := model.SourceInput{Name: "nas", Kind: model.SourceNodeExporter, URL: "http://nas:9100/metrics", SecretID: secretID}
	in.Normalize()
	src, err := s.CreateSource(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func TestSourcesAndSecrets(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	cs0, _ := s.ConfigState(ctx)
	sec, err := s.CreateSecret(ctx, "sec_aaaaaaaaaaaaaa", "nas", "basic", "u / ••", []byte("x"), 1)
	if err != nil {
		t.Fatal(err)
	}
	src := source(t, s, &sec.ID)
	cs1, _ := s.ConfigState(ctx)
	if cs1.Desired != cs0.Desired+1 {
		t.Fatalf("создание источника должно увеличить desired_revision: %d → %d", cs0.Desired, cs1.Desired)
	}
	// Секрет, на который ссылается источник, удалять нельзя: иначе сбор молча потеряет авторизацию.
	if err := s.DeleteSecret(ctx, sec.ID); !errors.Is(err, model.ErrInUse) {
		t.Fatalf("ожидался ErrInUse: %v", err)
	}
	list, _ := s.ListSecrets(ctx)
	if len(list) != 1 || len(list[0].UsedBy) != 1 || list[0].UsedBy[0] != src.ID {
		t.Fatalf("used_by: %+v", list)
	}
	if _, err := s.UpdateSource(ctx, src.ID, src.Revision+5, src.SourceInput); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("ревизия источника: %v", err)
	}
	missing := "sec_missing"
	in := src.SourceInput
	in.SecretID = &missing
	if _, err := s.UpdateSource(ctx, src.ID, src.Revision, in); err == nil {
		t.Fatal("ссылка на несуществующий секрет")
	}
	if err := s.UpdateSecret(ctx, sec.ID, "n", "basic", "m", []byte("y"), 1); err != nil {
		t.Fatal(err)
	}
	cs2, _ := s.ConfigState(ctx)
	if cs2.Desired != cs1.Desired+1 {
		t.Fatal("изменение секрета должно требовать перегенерации конфига")
	}
	if err := s.DeleteSource(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSecret(ctx, sec.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDataVersion(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	v0, _ := s.DataVersion(ctx)
	src := source(t, s, nil)
	v1, _ := s.DataVersion(ctx)
	if v0 == v1 {
		t.Fatal("создание источника должно менять версию данных")
	}
	// Статус сбора обновляется постоянно; он не должен инвалидировать предпросмотр импорта.
	if err := s.SaveSourceStatus(ctx, src.ID, time.Now(), time.Now(), "boom"); err != nil {
		t.Fatal(err)
	}
	if v2, _ := s.DataVersion(ctx); v2 != v1 {
		t.Fatalf("SaveSourceStatus изменил версию: %s → %s", v1, v2)
	}
	got, _ := s.GetSource(ctx, src.ID)
	if got.Status.Error != "boom" || got.Status.LastAttempt == nil {
		t.Fatalf("статус не сохранён: %+v", got.Status)
	}
	// Пустое время успеха не затирает прошлый успех.
	if err := s.SaveSourceStatus(ctx, src.ID, time.Now(), time.Time{}, "again"); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetSource(ctx, src.ID); got.Status.LastSuccess == nil {
		t.Fatal("last_success потерян")
	}
	service(t, s, "x")
	if v3, _ := s.DataVersion(ctx); v3 == v1 {
		t.Fatal("создание сервиса должно менять версию данных")
	}
}

func TestReplace(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	p, _ := s.CreatePage(ctx, pageInput("home"))
	sv := service(t, s, "old")
	version, _ := s.DataVersion(ctx)
	before, _ := s.Snapshot(ctx)

	scope := ReplaceScope{Pages: true, Services: true, Sources: true, Presets: true}
	next := before
	next.Services = []model.Service{{ID: "svc_new", Name: "new", URL: "http://new", Tags: []string{}, OpenMode: "new_tab"}}
	// Проверка ссылается на несуществующий сервис → FK-ошибка посреди транзакции.
	next.Checks = []model.Check{{ID: "chk_x", CheckInput: func() model.CheckInput { c := checkFor("svc_nope"); return c }()}}
	if err := s.Replace(ctx, scope, next, version); err == nil {
		t.Fatal("ожидалась ошибка")
	}
	after, _ := s.Snapshot(ctx)
	if len(after.Services) != 1 || after.Services[0].ID != sv.ID || len(after.Pages) != 1 || after.Pages[0].ID != p.ID {
		t.Fatalf("частичный импорт: %+v", after.Services)
	}

	next.Checks = nil
	if err := s.Replace(ctx, scope, next, "stale"); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("устаревшая версия: %v", err)
	}
	if err := s.Replace(ctx, scope, next, version); err != nil {
		t.Fatal(err)
	}
	after, _ = s.Snapshot(ctx)
	if len(after.Services) != 1 || after.Services[0].ID != "svc_new" {
		t.Fatalf("после замены: %+v", after.Services)
	}
	// id виджетов сохраняются при импорте, чтобы не ломать публичные ссылки.
	if after.Pages[0].Widgets[0].ID != p.Widgets[0].ID {
		t.Fatalf("id виджета не сохранён: %s → %s", p.Widgets[0].ID, after.Pages[0].Widgets[0].ID)
	}
}

func TestSessions(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	u := User{ID: "usr_1", Username: "admin", PasswordHash: "h"}
	if err := s.CreateFirstUser(ctx, u, "Мой дом"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateFirstUser(ctx, User{ID: "usr_2", Username: "b", PasswordHash: "h"}, "x"); !errors.Is(err, model.ErrConflict) {
		t.Fatal("второй администратор через setup")
	}
	if st, _ := s.GetSettings(ctx); st.Title != "Мой дом" {
		t.Fatal("название панели из setup")
	}
	if err := s.CreateSession(ctx, []byte("a"), u.ID, "csrf", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, []byte("b"), u.ID, "csrf", -time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionByHash(ctx, []byte("b")); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("просроченная сессия")
	}
	if err := s.CreateSession(ctx, []byte("c"), u.ID, "csrf", time.Hour); err != nil {
		t.Fatal(err)
	}
	// Смена пароля завершает все сессии, кроме текущей.
	if err := s.SetPassword(ctx, u.ID, "h2", []byte("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionByHash(ctx, []byte("a")); err != nil {
		t.Fatal("текущая сессия должна остаться")
	}
	if _, err := s.SessionByHash(ctx, []byte("c")); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("другие сессии должны завершиться")
	}
}

// Миграция 0003 снимает обёртку {"<драйвер>": {...}} с настроек устройств версии 2.
func TestMigrationDeviceConfig(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	for _, row := range [][3]string{
		{"dev_aaaaaaaaaaaaaa", "tuya", `{"tuya":{"device_id":"eb398c7f26966400abs3ju","version":"auto","schema":[]}}`},
		{"dev_bbbbbbbbbbbbbb", "http_json", `{"http_json":{"fields":[{"path":"a","key":"a","unit":"","scale":1}]}}`},
	} {
		if _, err := s.db.ExecContext(ctx, "INSERT INTO devices ("+deviceCols+", created_at) VALUES (?, 'x', ?, 'a', 30, 5, '{}', NULL, ?, 1, 1, 0)", row[0], row[1], row[2]); err != nil {
			t.Fatal(err)
		}
	}
	body, err := migrationsFS.ReadFile("migrations/0003_device_config.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListDevices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(list[0].Config) != `{"device_id":"eb398c7f26966400abs3ju","version":"auto","schema":[]}` || string(list[1].Config) != `{"fields":[{"path":"a","key":"a","unit":"","scale":1}]}` {
		t.Fatalf("после миграции: %s / %s", list[0].Config, list[1].Config)
	}
}
