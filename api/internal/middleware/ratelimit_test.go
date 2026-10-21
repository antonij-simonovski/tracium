package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tracium/api/internal/model"
)

func TestRateLimiterAllowsThenBlocks(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute, nil)
	handler := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	call := func() int {
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", nil)
		req.RemoteAddr = "10.0.0.1:5555"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr.Code
	}

	for i := 0; i < 3; i++ {
		if code := call(); code != http.StatusOK {
			t.Fatalf("request %d: code = %d, want 200", i+1, code)
		}
	}
	if code := call(); code != http.StatusTooManyRequests {
		t.Fatalf("4th request: code = %d, want 429", code)
	}
}

func TestRateLimiterIsolatesClients(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute, nil)
	handler := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	call := func(ip string) int {
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", nil)
		req.RemoteAddr = ip + ":5555"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr.Code
	}

	if code := call("10.0.0.1"); code != http.StatusOK {
		t.Fatalf("client A first request: %d, want 200", code)
	}
	if code := call("10.0.0.2"); code != http.StatusOK {
		t.Fatalf("client B first request should not be limited by client A: %d, want 200", code)
	}
	if code := call("10.0.0.1"); code != http.StatusTooManyRequests {
		t.Fatalf("client A second request: %d, want 429", code)
	}
}

// Without a trusted-proxy configuration, X-Forwarded-For is ignored entirely and
// the socket peer is the key — a client cannot use the header to change its key.
func TestClientIPIgnoresForwardedForByDefault(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.9:5555"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	if got := rl.clientIP(req); got != "198.51.100.9" {
		t.Fatalf("clientIP = %q, want socket peer 198.51.100.9", got)
	}
}

// When the peer is a trusted proxy, the real client is the right-most XFF entry
// that is not itself a trusted proxy (the proxy appends the peer it observed).
func TestClientIPUsesForwardedForFromTrustedProxy(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute, []string{"10.0.0.0/8"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:5555"
	// Attacker prepends a spoofed value; the trusted proxy (10.0.0.1) appended
	// the true client 203.0.113.7 on the right.
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.7")
	if got := rl.clientIP(req); got != "203.0.113.7" {
		t.Fatalf("clientIP = %q, want 203.0.113.7", got)
	}
}

// A caller cannot escape the limiter by varying X-Forwarded-For when it connects
// directly (its peer is not a trusted proxy): every request keys on the peer.
func TestClientIPForwardedForSpoofDoesNotBypassLimit(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute, []string{"10.0.0.0/8"})
	handler := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	call := func(xff string) int {
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", nil)
		req.RemoteAddr = "203.0.113.7:5555" // direct client, not a trusted proxy
		req.Header.Set("X-Forwarded-For", xff)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr.Code
	}

	if code := call("1.1.1.1"); code != http.StatusOK {
		t.Fatalf("first request: %d, want 200", code)
	}
	if code := call("2.2.2.2"); code != http.StatusTooManyRequests {
		t.Fatalf("spoofed second request: %d, want 429", code)
	}
}

// Hosted MCP connectors call from shared egress IPs, so accounts behind one IP
// must not share a bucket.
func TestRateLimiterPerPrincipalIsolatesAccountsOnOneIP(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute, nil)
	handler := rl.PerPrincipal()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	call := func(user string) int {
		req := httptest.NewRequest(http.MethodPost, "/v1/mcp", nil)
		req.RemoteAddr = "10.0.0.1:5555"
		req = req.WithContext(ContextWithPrincipal(req.Context(), &model.Principal{UserID: user}))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr.Code
	}

	if code := call("u1"); code != http.StatusOK {
		t.Fatalf("account A first request: %d, want 200", code)
	}
	if code := call("u2"); code != http.StatusOK {
		t.Fatalf("account B on the same IP: %d, want 200", code)
	}
	if code := call("u1"); code != http.StatusTooManyRequests {
		t.Fatalf("account A second request: %d, want 429", code)
	}
}
