package tsdb

import (
	"fmt"
	"maps"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/secrets"
	"go.yaml.in/yaml/v3"
)

const (
	SampleLimit     = 5000
	SeriesPerTarget = 20000
)

type scrapeFile struct {
	Global        scrapeGlobal `yaml:"global"`
	ScrapeConfigs []scrapeJob  `yaml:"scrape_configs"`
}

type scrapeGlobal struct {
	ScrapeInterval string `yaml:"scrape_interval"`
	ScrapeTimeout  string `yaml:"scrape_timeout"`
}

type scrapeJob struct {
	JobName         string              `yaml:"job_name"`
	Scheme          string              `yaml:"scheme,omitempty"`
	MetricsPath     string              `yaml:"metrics_path,omitempty"`
	Params          map[string][]string `yaml:"params,omitempty"`
	ScrapeInterval  string              `yaml:"scrape_interval"`
	ScrapeTimeout   string              `yaml:"scrape_timeout"`
	SampleLimit     int                 `yaml:"sample_limit,omitempty"`
	SeriesLimit     int                 `yaml:"series_limit,omitempty"`
	HonorLabels     bool                `yaml:"honor_labels"`
	FollowRedirects bool                `yaml:"follow_redirects"`
	ProxyURL        string              `yaml:"proxy_url,omitempty"`
	BasicAuth       *basicAuth          `yaml:"basic_auth,omitempty"`
	Authorization   *authorization      `yaml:"authorization,omitempty"`
	TLSConfig       *tlsConfig          `yaml:"tls_config,omitempty"`
	StaticConfigs   []staticConfig      `yaml:"static_configs"`
}

type basicAuth struct {
	Username     string `yaml:"username"`
	PasswordFile string `yaml:"password_file,omitempty"`
}

type authorization struct {
	Type            string `yaml:"type"`
	CredentialsFile string `yaml:"credentials_file"`
}

type tlsConfig struct {
	CAFile     string `yaml:"ca_file,omitempty"`
	ServerName string `yaml:"server_name,omitempty"`
}

type staticConfig struct {
	Targets []string          `yaml:"targets"`
	Labels  map[string]string `yaml:"labels"`
}

type RuntimeFile struct {
	Path    string
	Content []byte
}

type ScrapeInput struct {
	Sources        []model.Source
	Secrets        map[string]secrets.Payload
	InternalListen string
	VMListen       string
	EgressProxy    string
	RuntimeDir     string
}

func BuildScrapeConfig(in ScrapeInput) ([]byte, []RuntimeFile, error) {
	cfg := scrapeFile{
		Global: scrapeGlobal{ScrapeInterval: "15s", ScrapeTimeout: "5s"},
		ScrapeConfigs: []scrapeJob{
			systemJob(model.SystemSourceApp, in.InternalListen),
			systemJob(model.SystemSourceTSDB, in.VMListen),
		},
	}
	var files []RuntimeFile
	srcs := slices.Clone(in.Sources)
	slices.SortFunc(srcs, func(a, b model.Source) int { return strings.Compare(a.ID, b.ID) })
	for _, s := range srcs {
		if s.Enabled != nil && !*s.Enabled {
			continue
		}
		job, jobFiles, err := userJob(s, in)
		if err != nil {
			return nil, nil, fmt.Errorf("источник %s: %w", s.ID, err)
		}
		cfg.ScrapeConfigs = append(cfg.ScrapeConfigs, job)
		files = append(files, jobFiles...)
	}
	out, err := yaml.Marshal(cfg)
	return out, files, err
}

func systemJob(id, addr string) scrapeJob {
	return scrapeJob{
		JobName:        id,
		MetricsPath:    "/metrics",
		ScrapeInterval: "15s",
		ScrapeTimeout:  "5s",
		StaticConfigs:  []staticConfig{{Targets: []string{addr}, Labels: map[string]string{"source_id": id}}},
	}
}

func userJob(s model.Source, in ScrapeInput) (scrapeJob, []RuntimeFile, error) {
	u, err := url.Parse(s.URL)
	if err != nil {
		return scrapeJob{}, nil, err
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/metrics"
	}
	labels := map[string]string{}
	maps.Copy(labels, s.Labels)
	labels["source_id"] = s.ID
	job := scrapeJob{
		JobName:        s.ID,
		Scheme:         u.Scheme,
		MetricsPath:    path,
		ScrapeInterval: fmt.Sprintf("%ds", s.IntervalS),
		ScrapeTimeout:  fmt.Sprintf("%ds", s.TimeoutS),
		SampleLimit:    SampleLimit,
		SeriesLimit:    SeriesPerTarget,
		ProxyURL:       "http://" + in.EgressProxy,
		StaticConfigs:  []staticConfig{{Targets: []string{u.Host}, Labels: labels}},
	}
	if q := u.Query(); len(q) > 0 {
		job.Params = q
	}
	dir := filepath.Join(in.RuntimeDir, "secrets", s.ID)
	var files []RuntimeFile
	if s.SecretID != nil {
		p, ok := in.Secrets[*s.SecretID]
		if !ok {
			return scrapeJob{}, nil, fmt.Errorf("секрет %s недоступен", *s.SecretID)
		}
		switch {
		case p.Token != "":
			f := filepath.Join(dir, "token")
			files = append(files, RuntimeFile{f, []byte(p.Token)})
			job.Authorization = &authorization{Type: "Bearer", CredentialsFile: f}
		default:
			f := filepath.Join(dir, "password")
			files = append(files, RuntimeFile{f, []byte(p.Password)})
			job.BasicAuth = &basicAuth{Username: p.Username, PasswordFile: f}
		}
	}
	if s.TLS.CAPEM != "" || s.TLS.ServerName != "" {
		job.TLSConfig = &tlsConfig{ServerName: s.TLS.ServerName}
		if s.TLS.CAPEM != "" {
			f := filepath.Join(dir, "ca.pem")
			files = append(files, RuntimeFile{f, []byte(s.TLS.CAPEM)})
			job.TLSConfig.CAFile = f
		}
	}
	return job, files, nil
}
