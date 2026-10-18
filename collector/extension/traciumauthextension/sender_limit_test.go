package traciumauthextension

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/collector/client"
	"go.uber.org/zap"
	"google.golang.org/grpc/peer"
)

// On gRPC the auth interceptor runs before the collector fills client.Info.Addr,
// so senderOf must fall back to the gRPC peer — otherwise every gRPC sender is
// keyed as "unknown" and shares one rate-limit bucket.
func TestSenderOfFallsBackToGRPCPeer(t *testing.T) {
	// gRPC-style: no client.Info.Addr, but a peer on the context.
	ctx := peer.NewContext(context.Background(),
		&peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP("10.1.2.3"), Port: 5555}})
	if got := senderOf(ctx); got != "10.1.2.3" {
		t.Fatalf("senderOf(grpc peer) = %q, want 10.1.2.3", got)
	}

	// client.Info.Addr, when present (the HTTP path), takes precedence.
	if got := senderOf(ctxFromSender("192.0.2.9")); got != "192.0.2.9" {
		t.Fatalf("senderOf(client.Info) = %q, want 192.0.2.9", got)
	}

	// Neither present: the fixed fallback keeps such requests inside one bucket.
	if got := senderOf(context.Background()); got != "unknown" {
		t.Fatalf("senderOf(no addr) = %q, want unknown", got)
	}
}

// ctxFromSender builds a context whose client.Info carries a peer address, as a
// receiver's auth interceptor would, so the extension can charge the sender.
func ctxFromSender(ip string) context.Context {
	info := client.Info{Addr: &net.TCPAddr{IP: net.ParseIP(ip), Port: 4242}}
	return client.NewContext(context.Background(), info)
}

func TestSenderLimiterDisabledWhenNonPositive(t *testing.T) {
	if l := newSenderLimiter(0, time.Minute); l != nil {
		t.Fatal("limit 0 should disable the limiter")
	}
	if l := newSenderLimiter(5, 0); l != nil {
		t.Fatal("zero window should disable the limiter")
	}
	var nilLimiter *senderLimiter
	if !nilLimiter.allow("anyone") {
		t.Fatal("nil limiter must always allow")
	}
}

func TestSenderLimiterEnforcesAndResets(t *testing.T) {
	l := newSenderLimiter(2, time.Minute)
	if !l.allow("a") || !l.allow("a") {
		t.Fatal("first two attempts should be allowed")
	}
	if l.allow("a") {
		t.Fatal("third attempt should be blocked")
	}
	// A different sender has its own budget.
	if !l.allow("b") {
		t.Fatal("second sender should be independent")
	}
	// Force the window to have elapsed and confirm the budget refills.
	l.mu.Lock()
	l.seen["a"].start = time.Now().Add(-2 * time.Minute)
	l.mu.Unlock()
	if !l.allow("a") {
		t.Fatal("attempt after window reset should be allowed")
	}
}

// The tracked-sender map is a hard memory cap: once it is full of live windows,
// a flood of new source addresses is refused rather than admitted past the cap.
// An entry whose window has elapsed is swept first, so capacity frees up on its
// own as senders go idle.
func TestSenderLimiterEnforcesMemoryCap(t *testing.T) {
	l := newSenderLimiter(5, time.Minute)
	l.maxEntries = 2

	if !l.allow("a") || !l.allow("b") {
		t.Fatal("first two distinct senders should be admitted")
	}
	// Map is full of live windows: a third distinct sender cannot be inserted.
	if l.allow("c") {
		t.Fatal("new sender past the cap must be refused, not tracked")
	}
	if got := len(l.seen); got != 2 {
		t.Fatalf("tracked senders = %d, want 2 — the cap must not be exceeded", got)
	}
	// Age one window out; the next new sender's insert sweeps it and succeeds.
	l.mu.Lock()
	l.seen["a"].start = time.Now().Add(-2 * time.Minute)
	l.mu.Unlock()
	if !l.allow("c") {
		t.Fatal("new sender should be admitted after an idle window is swept")
	}
	if _, ok := l.seen["a"]; ok {
		t.Fatal("elapsed window should have been swept")
	}
}

// An abusive sender streaming distinct invalid keys is cut off after its budget
// without further calls to the API, so it cannot spend the shared verify budget.
func TestAbusiveSenderStopsHittingAPI(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	a := newAuth(&Config{
		Endpoint: srv.URL, Header: "authorization", Scheme: "Bearer",
		CacheTTL: time.Minute, CacheMaxEntries: 100, Timeout: 2 * time.Second,
		SenderVerifyLimit: 3, SenderVerifyWindow: time.Minute,
	}, zap.NewNop())

	ctx := ctxFromSender("10.0.0.9")
	for i, tok := range []string{"bad1", "bad2", "bad3", "bad4", "bad5"} {
		if _, err := a.Authenticate(ctx, map[string][]string{
			"authorization": {"Bearer " + tok},
		}); err == nil {
			t.Fatalf("attempt %d with invalid key should be rejected", i)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("API called %d times, want it capped at the sender limit of 3", got)
	}
}

// Cache hits do not consume the per-sender budget: a sender presenting one valid
// key repeatedly is served from cache and never throttled, even at limit 1.
func TestValidKeyReuseNotThrottled(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte(`{"workspace_id":"ws-1"}`))
	}))
	t.Cleanup(srv.Close)
	a := newAuth(&Config{
		Endpoint: srv.URL, Header: "authorization", Scheme: "Bearer",
		CacheTTL: time.Minute, CacheMaxEntries: 100, Timeout: 2 * time.Second,
		SenderVerifyLimit: 1, SenderVerifyWindow: time.Minute,
	}, zap.NewNop())

	ctx := ctxFromSender("10.0.0.7")
	sources := map[string][]string{"authorization": {"Bearer trc_good"}}
	for i := 0; i < 5; i++ {
		if _, err := a.Authenticate(ctx, sources); err != nil {
			t.Fatalf("reuse %d of a valid key should succeed, got %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("API called %d times, want 1 (only the cache miss)", got)
	}
	// The single miss consumed the sender's budget of 1, so a second, distinct
	// key from the same sender is now refused without an API call.
	if _, err := a.Authenticate(ctx, map[string][]string{
		"authorization": {"Bearer trc_other"},
	}); err == nil {
		t.Fatal("a distinct key past the sender budget should be rejected")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("API called %d times after budget exhausted, want still 1", got)
	}
}

func TestRateLimitedSenderFallsBackToStaleKey(t *testing.T) {
	var calls atomic.Int32
	a, _ := newTestAuth(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"workspace_id":"ws-1"}`))
	})
	a.cfg.MaxStaleAge = 10 * time.Minute
	a.senderLimiter = newSenderLimiter(1, time.Minute)
	token := "trc_valid"
	key := cacheKeyFor(token)
	a.store(key, cacheEntry{workspace: "ws-1", ok: true})
	a.items[key].Value.(*cacheNode).entry.expires = time.Now().Add(-time.Second)
	a.senderLimiter.allow("10.0.0.1")

	ctx, err := a.Authenticate(ctxFromSender("10.0.0.1"), map[string][]string{"authorization": {"Bearer " + token}})
	if err != nil {
		t.Fatalf("rate-limited sender with a just-expired valid key was rejected: %v", err)
	}
	if got := client.FromContext(ctx).Auth.GetAttribute(AttrWorkspace); got != "ws-1" {
		t.Fatalf("workspace = %v, want ws-1", got)
	}
	if calls.Load() != 0 {
		t.Fatal("rate-limited sender reached the verify API")
	}
}
