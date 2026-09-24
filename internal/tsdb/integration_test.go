package tsdb

import (
	"context"
	"database/sql"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/secrets"
	"github.com/fess932/homeLab/internal/store"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

type vmEnv struct {
	dbPath string
	store  *store.Store
	rec    *Reconciler
	sup    *Supervisor
	client *Client
}

func startVM(t *testing.T) vmEnv {
	t.Helper()
	bin := os.Getenv("HOMEDECK_VM_BINARY")
	if bin == "" {
		t.Skip("HOMEDECK_VM_BINARY не задан")
	}
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, err := secrets.NewBox(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	vmAddr := freeAddr(t)
	sup := NewSupervisor(Options{
		Binary: bin, DataDir: filepath.Join(dir, "metrics"), Listen: vmAddr, Retention: "1d",
		ScrapeConfig: filepath.Join(dir, "rt", "scrape.yaml"), MinFreeDisk: 1 << 20, MemoryPercent: 20,
		MaxScrapeSize: "16MiB", SeriesPerTarget: SeriesPerTarget,
	}, log)
	rec := &Reconciler{Store: st, Box: box, Sup: sup, Binary: bin, RuntimeDir: filepath.Join(dir, "rt"), RevisionsDir: filepath.Join(dir, "rev"),
		InternalListen: freeAddr(t), VMListen: vmAddr, EgressProxy: freeAddr(t), Log: log}
	if err := rec.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- sup.Run(runCtx) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	deadline := time.Now().Add(30 * time.Second)
	for !sup.Ready() {
		if time.Now().After(deadline) {
			t.Fatalf("VM не стартовала: %+v", sup.Status())
		}
		time.Sleep(100 * time.Millisecond)
	}
	return vmEnv{dbPath: filepath.Join(dir, "app.db"), store: st, rec: rec, sup: sup, client: NewClient(vmAddr)}
}

func TestIntegrationReconcile(t *testing.T) {
	env := startVM(t)
	ctx := context.Background()
	cs, _ := env.store.ConfigState(ctx)
	if cs.Applied != cs.Desired || cs.Error != "" {
		t.Fatalf("после Prepare: %+v", cs)
	}

	in := model.SourceInput{Name: "nas", Kind: model.SourceNodeExporter, URL: "http://192.0.2.10:9100/metrics"}
	in.Normalize()
	src, err := env.store.CreateSource(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.rec.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	cs, _ = env.store.ConfigState(ctx)
	if cs.Applied != cs.Desired || cs.Error != "" {
		t.Fatalf("после Apply: %+v", cs)
	}
	good, _ := os.ReadFile(env.rec.ConfigPath())
	if !strings.Contains(string(good), src.ID) {
		t.Fatal("новый источник не попал в конфиг")
	}
	// Цель появилась в самой VM — reload действительно применён.
	waitTarget(t, env.client, src.ID)

	// Заведомо некорректный конфиг отклоняется dry-run'ом.
	if err := env.rec.validate(ctx, []byte("scrape_configs:\n  - job_name: a\n    bogus_field: 1\n"), nil); err == nil || !strings.Contains(err.Error(), "bogus_field") {
		t.Fatalf("dry-run должен отклонить неизвестное поле: %v", err)
	}

	// Источник, обошедший валидацию API (например, из старой версии), даёт невалидный конфиг:
	// применение не проходит, рабочий конфиг остаётся, ошибка видна в состоянии.
	in2 := in
	in2.Name = "broken"
	bad, err := env.store.CreateSource(ctx, in2)
	if err != nil {
		t.Fatal(err)
	}
	if err := corruptSource(env.dbPath, bad.ID); err != nil {
		t.Fatal(err)
	}
	if err := env.rec.Apply(ctx); err == nil {
		t.Fatal("ожидалась ошибка применения")
	}
	cs2, _ := env.store.ConfigState(ctx)
	if cs2.Applied != cs.Applied || cs2.Error == "" || cs2.Desired == cs2.Applied {
		t.Fatalf("после неудачи: %+v", cs2)
	}
	after, _ := os.ReadFile(env.rec.ConfigPath())
	if string(after) != string(good) {
		t.Fatal("рабочий конфиг должен остаться прежним")
	}
	if !env.sup.Ready() {
		t.Fatal("VM должна продолжать работать")
	}
}

func corruptSource(dbPath, id string) error {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer db.Close()
	// id совпадает с системным job — VM отклонит дубликат job_name.
	_, err = db.Exec("UPDATE sources SET id = 'tsdb' WHERE id = ?", id)
	return err
}

func waitTarget(t *testing.T, c *Client, sourceID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		targets, err := c.Targets(context.Background())
		if err == nil {
			for _, tg := range targets {
				if tg.SourceID == sourceID {
					return
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("цель %s не появилась в VM", sourceID)
}

func TestIntegrationPresets(t *testing.T) {
	env := startVM(t)
	ctx := context.Background()
	end := time.Now()
	start := end.Add(-time.Hour)
	for _, p := range model.BuiltinPresets() {
		vars := map[string]string{}
		for _, v := range p.Vars {
			vars[v] = "x_1"
		}
		expr, err := model.ExpandPreset(p.Expression, vars)
		if err != nil {
			t.Fatal(err)
		}
		s, e, step := Grid(start, end, 300, 0)
		if _, err := env.client.QueryRange(ctx, expr, s, e, step); err != nil {
			t.Errorf("%s: %v", p.ID, err)
		}
		if _, err := env.client.Query(ctx, expr, end); err != nil {
			t.Errorf("%s instant: %v", p.ID, err)
		}
	}
	if _, err := env.client.Query(ctx, "sum(", end); !isCode(err, CodeBadQuery) {
		t.Errorf("синтаксическая ошибка: %v", err)
	}
}

func TestIntegrationTooManySeries(t *testing.T) {
	env := startVM(t)
	ctx := context.Background()
	// Системный job tsdb собирает метрики самой VM — это заведомо больше 100 рядов.
	deadline := time.Now().Add(40 * time.Second)
	for {
		res, err := env.client.Query(ctx, `count({__name__=~"vm_.+"})`, time.Now())
		if err == nil && len(res.Samples) == 1 && res.Samples[0].Value != nil && *res.Samples[0].Value > 100 {
			break
		}
		if time.Now().After(deadline) {
			t.Skip("VM ещё не собрала собственные метрики")
		}
		time.Sleep(time.Second)
	}
	_, err := env.client.Query(ctx, `{__name__=~"vm_.+"}`, time.Now())
	if !isCode(err, CodeTooMany) {
		t.Fatalf("ожидался too_many_series: %v", err)
	}
}
