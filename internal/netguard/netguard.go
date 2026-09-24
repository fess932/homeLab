package netguard

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

var ErrForbidden = errors.New("адрес запрещён политикой исходящих соединений")

type ForbiddenError struct{ Addr netip.Addr }

func (e *ForbiddenError) Error() string {
	return fmt.Sprintf("адрес %s запрещён: loopback, link-local, metadata и служебные сети недоступны источникам", e.Addr)
}

func (e *ForbiddenError) Unwrap() error { return ErrForbidden }

var denied = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("100.100.100.200/32"),
	netip.MustParsePrefix("192.0.0.192/32"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
	netip.MustParsePrefix("fd00:ec2::254/128"),
}

var nat64 = netip.MustParsePrefix("64:ff9b::/96")

func Check(a netip.Addr) error {
	a = a.Unmap()
	if nat64.Contains(a) {
		b := a.As16()
		a = netip.AddrFrom4([4]byte(b[12:]))
	}
	for _, p := range denied {
		if p.Contains(a) {
			return &ForbiddenError{Addr: a}
		}
	}
	return nil
}

func control(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("адрес соединения %q: %w", address, err)
	}
	return Check(ap.Addr())
}

func Dialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second, Control: control}
}

func Transport(tlsCfg *tls.Config) *http.Transport {
	d := Dialer(10 * time.Second)
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           d.DialContext,
		TLSClientConfig:       tlsCfg,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		MaxIdleConns:          50,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     true,
	}
}

func NoRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

type Proxy struct {
	transport *http.Transport
	dialer    *net.Dialer
	sem       chan struct{}
	log       *slog.Logger
}

func NewProxy(log *slog.Logger) *Proxy {
	t := Transport(nil)
	t.DisableKeepAlives = false
	return &Proxy{transport: t, dialer: Dialer(10 * time.Second), sem: make(chan struct{}, 64), log: log}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	select {
	case p.sem <- struct{}{}:
		defer func() { <-p.sem }()
	default:
		http.Error(w, "egress proxy overloaded", http.StatusServiceUnavailable)
		return
	}
	if r.Method == http.MethodConnect {
		p.connect(w, r)
		return
	}
	if r.URL.Scheme != "http" || r.URL.Host == "" {
		http.Error(w, "absolute http URL required", http.StatusBadRequest)
		return
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	out.Header.Del("Proxy-Connection")
	out.Header.Del("Proxy-Authorization")
	resp, err := p.transport.RoundTrip(out)
	if err != nil {
		p.fail(w, r.URL.Host, err)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (p *Proxy) connect(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	upstream, err := p.dialer.DialContext(ctx, "tcp", r.Host)
	cancel()
	if err != nil {
		p.fail(w, r.Host, err)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		upstream.Close()
		http.Error(w, "hijack unsupported", http.StatusInternalServerError)
		return
	}
	client, buf, err := hj.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if n := buf.Reader.Buffered(); n > 0 {
		pending, _ := buf.Peek(n)
		_, _ = upstream.Write(pending)
	}
	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if c, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = c.CloseWrite()
		}
		done <- struct{}{}
	}
	go pipe(upstream, client)
	go pipe(client, upstream)
	<-done
	<-done
	upstream.Close()
	client.Close()
}

func (p *Proxy) fail(w http.ResponseWriter, host string, err error) {
	status := http.StatusBadGateway
	if errors.Is(err, ErrForbidden) {
		status = http.StatusForbidden
	}
	p.log.Debug("egress denied or failed", "host", host, "err", err)
	http.Error(w, err.Error(), status)
}
