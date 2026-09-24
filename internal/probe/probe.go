package probe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/fess932/homeLab/internal/model"
	"github.com/fess932/homeLab/internal/netguard"
)

type Result struct {
	OK         bool
	Duration   time.Duration
	HTTPStatus int
	Kind       string
	Error      string
}

type Prober interface {
	Probe(ctx context.Context, c model.Check) Result
}

type NetProber struct {
	UserAgent string
}

func (p NetProber) Probe(ctx context.Context, c model.Check) Result {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.TimeoutS)*time.Second)
	defer cancel()
	start := time.Now()
	var r Result
	switch c.Kind {
	case "tcp":
		r = p.tcp(ctx, c)
	default:
		r = p.http(ctx, c)
	}
	r.Duration = time.Since(start)
	return r
}

func (p NetProber) tcp(ctx context.Context, c model.Check) Result {
	conn, err := netguard.Dialer(time.Duration(c.TimeoutS)*time.Second).DialContext(ctx, "tcp", c.Target)
	if err != nil {
		kind, msg := Classify(err)
		return Result{Kind: kind, Error: msg}
	}
	_ = conn.Close()
	return Result{OK: true}
}

func (p NetProber) http(ctx context.Context, c model.Check) Result {
	tlsCfg, err := TLSConfig(c.CAPEM, "")
	if err != nil {
		return Result{Kind: KindTLS, Error: err.Error()}
	}
	tr := netguard.Transport(tlsCfg)
	tr.DisableKeepAlives = true
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, CheckRedirect: netguard.NoRedirects}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Target, nil)
	if err != nil {
		return Result{Kind: KindConnect, Error: err.Error()}
	}
	req.Header.Set("User-Agent", p.UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		kind, msg := Classify(err)
		return Result{Kind: kind, Error: msg}
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
	ranges, _ := model.ParseStatusRanges(c.ExpectedStatus)
	if !model.StatusMatches(ranges, resp.StatusCode) {
		return Result{HTTPStatus: resp.StatusCode, Kind: KindHTTPStatus, Error: fmt.Sprintf("код ответа %d не входит в %s", resp.StatusCode, c.ExpectedStatus)}
	}
	return Result{OK: true, HTTPStatus: resp.StatusCode}
}

func TLSConfig(caPEM, serverName string) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}
	if caPEM == "" {
		return cfg, nil
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM([]byte(caPEM)) {
		return nil, fmt.Errorf("некорректный CA-сертификат")
	}
	cfg.RootCAs = pool
	return cfg, nil
}
