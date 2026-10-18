package traciumauthextension

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

type cancellationTransport func(*http.Request) (*http.Response, error)

func (f cancellationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCanceledVerificationDoesNotRefreshCache(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "disconnect"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			cfg := createDefaultConfig().(*Config)
			cfg.Endpoint = "http://verify.invalid"
			cfg.MaxStaleAge = time.Minute // cancellation must also be safe with fallback enabled
			a := newAuth(cfg, zap.NewNop())
			token := "trc_previously_valid_now_revoked"
			key := cacheKeyFor(token)
			a.store(key, cacheEntry{workspace: "private-workspace", ok: true})
			expired := time.Now().Add(-time.Second)
			a.items[key].Value.(*cacheNode).entry.expires = expired
			entered := make(chan struct{})
			var calls atomic.Int32
			a.client.Transport = cancellationTransport(func(r *http.Request) (*http.Response, error) {
				if calls.Add(1) == 1 {
					close(entered)
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("revoked")), Header: make(http.Header)}, nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
			}
			defer cancel()
			done := make(chan error, 1)
			headers := map[string][]string{"authorization": {"Bearer " + token}}
			go func() { _, err := a.Authenticate(ctx, headers); done <- err }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("verification did not start")
			}
			if !deadline {
				cancel()
			}
			if err := <-done; err == nil {
				t.Fatal("canceled verification was accepted")
			}
			if got := a.items[key].Value.(*cacheNode).entry.expires; !got.Equal(expired) {
				t.Fatal("cancellation refreshed expired authorization")
			}
			if _, err := a.Authenticate(context.Background(), headers); err == nil {
				t.Fatal("follow-up accepted a revoked key")
			}
			if calls.Load() != 2 {
				t.Fatal("follow-up must request the backend's revocation decision")
			}
		})
	}
}

func TestStaleAuthorizationHasAbsoluteOptInDeadline(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	a := newAuth(cfg, zap.NewNop())
	a.store("key", cacheEntry{workspace: "ws-1", ok: true})
	if _, ok := a.lookupStale("key"); ok {
		t.Fatal("stale authorization must be disabled by default")
	}
	cfg.MaxStaleAge = time.Minute
	entry := a.items["key"].Value.(*cacheNode).entry
	entry.verifiedAt = time.Now().Add(-time.Minute + time.Second)
	a.items["key"].Value.(*cacheNode).entry = entry
	for i := 0; i < 3; i++ {
		a.storeStale("key", entry)
	}
	got := a.items["key"].Value.(*cacheNode).entry
	if !got.verifiedAt.Equal(entry.verifiedAt) || got.expires.After(entry.verifiedAt.Add(cfg.MaxStaleAge)) {
		t.Fatal("stale recaching extended the absolute trust deadline")
	}
	got.verifiedAt = time.Now().Add(-2 * time.Minute)
	a.items["key"].Value.(*cacheNode).entry = got
	if _, ok := a.lookupStale("key"); ok {
		t.Fatal("authorization older than the absolute age bound was accepted")
	}
}

func TestCanceledLeaderDoesNotFailWaiters(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.Endpoint = "http://verify.invalid"
	a := newAuth(cfg, zap.NewNop())
	entered := make(chan struct{})
	var calls atomic.Int32
	a.client.Transport = cancellationTransport(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"workspace_id":"ws-1"}`)), Header: make(http.Header)}, nil
	})
	headers := map[string][]string{"authorization": {"Bearer trc_shared"}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leader := make(chan error, 1)
	go func() { _, err := a.Authenticate(ctx, headers); leader <- err }()
	<-entered

	const n = 4
	waiters := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() { _, err := a.Authenticate(context.Background(), headers); waiters <- err }()
	}
	time.Sleep(50 * time.Millisecond)
	cancel()

	if err := <-leader; err == nil {
		t.Fatal("canceled leader was accepted")
	}
	for i := 0; i < n; i++ {
		if err := <-waiters; err != nil {
			t.Fatalf("waiter failed after leader cancellation: %v", err)
		}
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("verify calls = %d, want 2", got)
	}
}
