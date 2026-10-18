package middleware

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNamedProxyIsolatesClientsAndRejectsSpoofing(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute, []string{"dns:dashboard"})
	lookups := 0
	rl.lookupProxy = func(_ context.Context, name string) ([]net.IPAddr, error) {
		lookups++
		if name != "dashboard" {
			t.Fatalf("unexpected proxy name: %s", name)
		}
		return []net.IPAddr{{IP: net.ParseIP("10.0.0.3")}}, nil
	}
	h := rl.Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	request := func(peer, forwarded string) int {
		r := httptest.NewRequest("POST", "/v1/auth/login", nil)
		r.RemoteAddr = net.JoinHostPort(peer, "4000")
		r.Header.Set("X-Forwarded-For", forwarded)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if request("10.0.0.3", "203.0.113.1") != 200 || request("10.0.0.3", "203.0.113.1") != 429 {
		t.Fatal("first client was not limited")
	}
	if request("10.0.0.3", "203.0.113.2") != 200 {
		t.Fatal("second browser shared the first browser's bucket")
	}
	if request("10.0.0.4", "198.51.100.1") != 200 || request("10.0.0.4", "198.51.100.2") != 429 {
		t.Fatal("untrusted container bypassed limits with forged forwarding headers")
	}
	if request("10.0.0.3", "198.51.100.9, 203.0.113.1") != 429 {
		t.Fatal("prepended spoof bypassed the real client's limit")
	}
	if lookups != 1 {
		t.Fatalf("DNS was not cached: %d lookups", lookups)
	}
}

func TestNamedProxyRefreshDoesNotBlockOtherRequests(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute, []string{"dns:dashboard"})
	block := false
	started, release := make(chan struct{}), make(chan struct{})
	rl.lookupProxy = func(context.Context, string) ([]net.IPAddr, error) {
		if block {
			close(started)
			<-release
		}
		return []net.IPAddr{{IP: net.ParseIP("10.0.0.3")}}, nil
	}
	if !rl.isTrustedProxy("10.0.0.3") {
		t.Fatal("current proxy was not trusted")
	}

	block = true
	rl.proxyExpires = time.Time{}
	refreshed := make(chan bool)
	go func() { refreshed <- rl.isTrustedProxy("10.0.0.3") }()
	<-started

	checked := make(chan bool)
	go func() { checked <- rl.isTrustedProxy("10.0.0.3") }()
	select {
	case trusted := <-checked:
		if !trusted {
			t.Fatal("request during refresh lost the previous snapshot")
		}
	case <-time.After(time.Second):
		t.Fatal("request blocked behind the DNS refresh")
	}
	close(release)
	if !<-refreshed {
		t.Fatal("refreshing request did not trust the proxy")
	}
}

func TestNamedProxyRefreshRemovesOldAndUnresolvableAddresses(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute, []string{"dns:dashboard-peers.namespace.svc"})
	address := "10.0.0.3"
	rl.lookupProxy = func(context.Context, string) ([]net.IPAddr, error) {
		if address == "" {
			return nil, errors.New("DNS unavailable")
		}
		return []net.IPAddr{{IP: net.ParseIP(address)}}, nil
	}
	if !rl.isTrustedProxy(address) {
		t.Fatal("current proxy was not trusted")
	}
	address = "10.0.0.5"
	rl.proxyExpires = time.Time{}
	if rl.isTrustedProxy("10.0.0.3") || !rl.isTrustedProxy(address) {
		t.Fatal("trust did not follow the replacement proxy")
	}
	address = ""
	rl.proxyExpires = time.Time{}
	if rl.isTrustedProxy("10.0.0.5") {
		t.Fatal("failed discovery retained stale trust")
	}
}
