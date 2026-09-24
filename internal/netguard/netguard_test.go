package netguard

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCheck(t *testing.T) {
	cases := []struct {
		addr    string
		allowed bool
	}{
		{"192.168.1.10", true},
		{"10.0.0.5", true},
		{"172.16.3.4", true},
		{"8.8.8.8", true},
		{"fd12:3456::1", true},
		{"127.0.0.1", false},
		{"127.10.0.1", false},
		{"0.0.0.0", false},
		{"169.254.169.254", false},
		{"100.100.100.200", false},
		{"::1", false},
		{"::", false},
		{"fe80::1", false},
		{"fd00:ec2::254", false},
		{"224.0.0.1", false},
		// IPv4-mapped IPv6 обходит проверку, если не сделать Unmap.
		{"::ffff:127.0.0.1", false},
		// NAT64 с вложенным loopback.
		{"64:ff9b::7f00:1", false},
		{"64:ff9b::c0a8:10a", true},
	}
	for _, c := range cases {
		err := Check(netip.MustParseAddr(c.addr))
		if (err == nil) != c.allowed {
			t.Errorf("%s: allowed=%v, err=%v", c.addr, c.allowed, err)
		}
		if err != nil && !errors.Is(err, ErrForbidden) {
			t.Errorf("%s: ошибка должна оборачивать ErrForbidden", c.addr)
		}
	}
}

func TestTransportBlocksLoopbackAfterDNS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	// localhost резолвится в loopback: проверка должна срабатывать на фактическом адресе соединения, а не на имени.
	target := "http://localhost:" + u.Port()
	client := &http.Client{Transport: Transport(nil), Timeout: 2 * time.Second}
	_, err := client.Get(target)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("ожидался запрет, получено %v", err)
	}
}

func TestProxyDeniesLoopbackConnect(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer backend.Close()
	proxy := httptest.NewServer(NewProxy(slog.New(slog.DiscardHandler)))
	defer proxy.Close()

	conn, err := net.Dial("tcp", strings.TrimPrefix(proxy.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	host := strings.TrimPrefix(backend.URL, "http://")
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", host, host)
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("CONNECT к loopback: статус %d, ожидался 403", resp.StatusCode)
	}
}

func TestProxyDeniesLoopbackForward(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "secret")
	}))
	defer backend.Close()
	proxy := httptest.NewServer(NewProxy(slog.New(slog.DiscardHandler)))
	defer proxy.Close()
	pu, _ := url.Parse(proxy.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu)}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, backend.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("forward к loopback: статус %d, ожидался 403", resp.StatusCode)
	}
}
