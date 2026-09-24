package main

import (
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/fess932/homeLab/internal/app"
	"github.com/lmittmann/tint"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/mattn/go-isatty"
	"github.com/muesli/termenv"
)

//go:embed logo.txt
var logo string

// Интерактивный терминал получает заставку и цветные логи, всё остальное
// (Docker, файл, pipe) — JSON для сборщиков логов.
func isConsole() bool {
	fd := os.Stdout.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

func newLogger(console bool, level slog.Level) *slog.Logger {
	if !console {
		return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	}
	return slog.New(tint.NewTextHandler(os.Stdout, &tint.Options{Level: level, TimeFormat: time.TimeOnly, NoColor: termenv.EnvNoColor()}))
}

// enableColors включает обработку ANSI-последовательностей в консоли Windows.
func enableColors() (restore func()) {
	reset, err := termenv.EnableVirtualTerminalProcessing(termenv.NewOutput(os.Stdout))
	if err != nil {
		return func() {}
	}
	return func() { _ = reset() }
}

func printBanner(s app.Started) {
	r := lipgloss.NewRenderer(os.Stdout)
	from, _ := colorful.Hex("#22d3ee")
	to, _ := colorful.Hex("#a855f7")

	lines := strings.Split(strings.TrimRight(logo, "\n"), "\n")
	width := 0
	for _, l := range lines {
		width = max(width, utf8.RuneCountInString(l))
	}
	var b strings.Builder
	b.WriteByte('\n')
	for _, l := range lines {
		b.WriteString("  ")
		for i, ch := range []rune(l) {
			c := from.BlendLuv(to, float64(i)/float64(width-1)).Hex()
			b.WriteString(r.NewStyle().Foreground(lipgloss.Color(c)).Render(string(ch)))
		}
		b.WriteByte('\n')
	}

	accent := r.NewStyle().Foreground(lipgloss.Color("#22d3ee"))
	label := r.NewStyle().Bold(true)
	dim := r.NewStyle().Faint(true)
	row := func(name, value string) {
		fmt.Fprintf(&b, "  %s %s %s\n", accent.Render("➜"), label.Render(fmt.Sprintf("%-12s", name)), value)
	}

	fmt.Fprintf(&b, "\n  %s %s\n\n", label.Render("Панель и мониторинг homelab"), dim.Render(s.Version))
	row("Локально", accent.Underline(true).Render(s.URLs[0]))
	for _, u := range s.URLs[1:] {
		row("В сети", accent.Underline(true).Render(u))
	}
	row("Данные", s.DataDir)
	if s.SetupToken != "" {
		row("Setup-token", r.NewStyle().Foreground(lipgloss.Color("#facc15")).Render(s.SetupToken)+dim.Render("  ← введите при первом входе"))
	}
	b.WriteByte('\n')
	fmt.Print(b.String())
}
