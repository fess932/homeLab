package config

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen          string
	DataDir         string
	RuntimeDir      string
	Retention       string
	VMBinary        string
	VMListen        string
	InternalListen  string
	EgressListen    string
	SecretKeyFile   string
	TrustedProxies  []netip.Prefix
	AllowedHosts    []string
	MinFreeDisk     int64
	VMMemoryPercent int
	ScrapeInterval  time.Duration
	ScrapeTimeout   time.Duration
	LogLevel        string
	Version         string
}

var retentionRe = regexp.MustCompile(`^[1-9][0-9]*[hdwy]$`)

const (
	DefaultRetention = "50y"
	// MaxRetention — самый долгий срок хранения, который принимает VictoriaMetrics.
	MaxRetention = "100y"
)

// retentionHours переводит срок хранения в часы, чтобы сравнить его с пределом.
func retentionHours(r string) int {
	n, _ := strconv.Atoi(r[:len(r)-1])
	return n * map[byte]int{'h': 1, 'd': 24, 'w': 24 * 7, 'y': 24 * 365}[r[len(r)-1]]
}

func FromEnv(version string) (Config, error) {
	c := Config{
		Listen:          env("HOMEDECK_LISTEN", ":8080"),
		DataDir:         env("HOMEDECK_DATA_DIR", "/data"),
		RuntimeDir:      env("HOMEDECK_RUNTIME_DIR", "/tmp/homedeck"),
		// По умолчанию история хранится полвека, то есть фактически не удаляется.
		Retention:       env("HOMEDECK_RETENTION", DefaultRetention),
		VMBinary:        env("HOMEDECK_VM_BINARY", "/usr/local/bin/victoria-metrics"),
		VMListen:        env("HOMEDECK_VM_LISTEN", "127.0.0.1:8428"),
		InternalListen:  env("HOMEDECK_INTERNAL_LISTEN", "127.0.0.1:9091"),
		EgressListen:    env("HOMEDECK_EGRESS_LISTEN", "127.0.0.1:9092"),
		SecretKeyFile:   os.Getenv("HOMEDECK_SECRET_KEY_FILE"),
		LogLevel:        env("HOMEDECK_LOG_LEVEL", "info"),
		ScrapeInterval:  15 * time.Second,
		ScrapeTimeout:   5 * time.Second,
		VMMemoryPercent: 40,
		Version:         version,
	}
	if c.SecretKeyFile == "" {
		c.SecretKeyFile = filepath.Join(c.DataDir, "secrets.key")
	}
	if !retentionRe.MatchString(c.Retention) || len(c.Retention) > 8 {
		return c, fmt.Errorf("HOMEDECK_RETENTION: %q, ожидается число с суффиксом h, d, w или y", c.Retention)
	}
	if retentionHours(c.Retention) > retentionHours(MaxRetention) {
		return c, fmt.Errorf("HOMEDECK_RETENTION: %q, VictoriaMetrics хранит не дольше %s", c.Retention, MaxRetention)
	}
	var err error
	if c.MinFreeDisk, err = parseSize(env("HOMEDECK_MIN_FREE_DISK", "512MiB")); err != nil {
		return c, fmt.Errorf("HOMEDECK_MIN_FREE_DISK: %w", err)
	}
	if v := os.Getenv("HOMEDECK_VM_MEMORY_PERCENT"); v != "" {
		if c.VMMemoryPercent, err = strconv.Atoi(v); err != nil || c.VMMemoryPercent < 5 || c.VMMemoryPercent > 90 {
			return c, errors.New("HOMEDECK_VM_MEMORY_PERCENT: ожидается целое от 5 до 90")
		}
	}
	for s := range strings.SplitSeq(os.Getenv("HOMEDECK_TRUSTED_PROXIES"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		p, err := parsePrefix(s)
		if err != nil {
			return c, fmt.Errorf("HOMEDECK_TRUSTED_PROXIES: %w", err)
		}
		c.TrustedProxies = append(c.TrustedProxies, p)
	}
	for s := range strings.SplitSeq(os.Getenv("HOMEDECK_ALLOWED_HOSTS"), ",") {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			c.AllowedHosts = append(c.AllowedHosts, s)
		}
	}
	return c, nil
}

func (c Config) DBPath() string         { return filepath.Join(c.DataDir, "app.db") }
func (c Config) MetricsDir() string     { return filepath.Join(c.DataDir, "metrics") }
func (c Config) AssetsDir() string      { return filepath.Join(c.DataDir, "assets") }
func (c Config) RevisionsDir() string   { return filepath.Join(c.DataDir, "config-revisions") }
func (c Config) SetupTokenPath() string { return filepath.Join(c.DataDir, "setup-token") }
func (c Config) LockPath() string       { return filepath.Join(c.DataDir, ".lock") }

func env(k, def string) string {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		return v
	}
	return def
}

func parsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		return netip.ParsePrefix(s)
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}

func parseSize(s string) (int64, error) {
	units := []struct {
		suffix string
		mul    int64
	}{{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"GB", 1e9}, {"MB", 1e6}, {"KB", 1e3}, {"B", 1}}
	for _, u := range units {
		if n, ok := strings.CutSuffix(s, u.suffix); ok {
			v, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
			if err != nil || v < 0 {
				return 0, fmt.Errorf("некорректный размер %q", s)
			}
			return v * u.mul, nil
		}
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("некорректный размер %q", s)
	}
	return v, nil
}
