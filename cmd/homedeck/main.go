package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	_ "time/tzdata"

	"github.com/fess932/homeLab/internal/app"
	"github.com/fess932/homeLab/internal/config"
	"github.com/fess932/homeLab/internal/store"
	"github.com/fess932/homeLab/web"
)

var version = "dev"

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	cfg, err := config.FromEnv(version)
	if err != nil {
		fatal(err)
	}
	switch cmd {
	case "serve":
		serve(cfg)
	case "healthcheck":
		if err := app.Healthcheck(context.Background(), cfg.Listen); err != nil {
			fatal(err)
		}
	case "setup-token":
		b, err := os.ReadFile(cfg.SetupTokenPath())
		if errors.Is(err, os.ErrNotExist) {
			fatal(errors.New("setup-token отсутствует: первичная настройка уже выполнена или сервер ещё не запускался"))
		}
		if err != nil {
			fatal(err)
		}
		fmt.Println(strings.TrimSpace(string(b)))
	case "version":
		fmt.Printf("homedeck %s, schema %d\n", version, store.SchemaVersion())
	default:
		fmt.Fprintf(os.Stderr, "usage: homedeck [serve|healthcheck|setup-token|version]\n")
		os.Exit(2)
	}
}

func serve(cfg config.Config) {
	level := slog.LevelInfo
	_ = level.UnmarshalText([]byte(cfg.LogLevel))
	console := isConsole()
	var onStart func(app.Started)
	if console {
		defer enableColors()()
		onStart = printBanner
	}
	log := newLogger(console, level)
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := app.Run(ctx, cfg, web.FS(), log, onStart); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "homedeck:", err)
	os.Exit(1)
}
