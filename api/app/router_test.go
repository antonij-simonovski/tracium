package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/tracium/api/extension"
	"github.com/tracium/api/internal/apikey"
	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/testing/mocks"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The ingest verify endpoint must have its own rate-limit bucket, not share
// login's: the collector is a single IP whose verify traffic would otherwise
// exhaust the tiny login allowance and block verification of legitimate keys.
func TestVerifyRateLimitIsSeparateFromLogin(t *testing.T) {
	var cfg Config
	cfg.Default()
	cfg.Auth.RateLimitPerMinute = 1
	cfg.Auth.VerifyRateLimitPerMinute = 2

	repo := repository{mocks.NewMockTraceRepository(), &mocks.MockMetricsRepository{}}
	router := newRouter(cfg, repo, &mocks.MockWorkspaceStore{}, nil, nil, tokenAuth{}, nil, apikey.NewService(nil), nil, Options{})

	do := func(path, body string) int {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.RemoteAddr = "192.0.2.10:5555"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w.Code
	}

	// Exhaust login's bucket (limit 1): first request passes the limiter (400 for
	// the bad body), the second is throttled.
	if code := do("/v1/auth/login", "not-json"); code == http.StatusTooManyRequests {
		t.Fatalf("first login unexpectedly throttled: %d", code)
	}
	if code := do("/v1/auth/login", "not-json"); code != http.StatusTooManyRequests {
		t.Fatalf("second login should be throttled, got %d", code)
	}

	// Login being exhausted must not touch verify: it has its own budget of 2.
	// Empty key yields 401 without reaching the store.
	if code := do("/v1/ingest/keys/verify", `{}`); code == http.StatusTooManyRequests {
		t.Fatalf("verify throttled by login's bucket: %d", code)
	}
	if code := do("/v1/ingest/keys/verify", `{}`); code == http.StatusTooManyRequests {
		t.Fatalf("verify throttled before its own limit: %d", code)
	}
	// The third verify exceeds verify's own limit of 2.
	if code := do("/v1/ingest/keys/verify", `{}`); code != http.StatusTooManyRequests {
		t.Fatalf("third verify should hit verify's own limit, got %d", code)
	}
}

type repository struct {
	*mocks.MockTraceRepository
	*mocks.MockMetricsRepository
}

type tokenAuth map[string]string

func (a tokenAuth) Authenticate(_ context.Context, token string) (*model.Principal, error) {
	if id, ok := a[token]; ok {
		return &model.Principal{UserID: id}, nil
	}
	return nil, errors.New("invalid token")
}

func TestExtensionsShareAuthAndCannotReplaceCoreRoutes(t *testing.T) {
	const token = "valid"
	var cfg Config
	cfg.Default()
	opts := Options{Extensions: []Extension{{Name: "example", Routes: func(r chi.Router, s extension.Services) {
		r.Get("/identity", func(w http.ResponseWriter, r *http.Request) {
			p, ok := extension.PrincipalFromContext(r.Context())
			if !ok || p.UserID != "user" {
				t.Error("missing shared identity")
			}
			w.WriteHeader(204)
		})
	}, Webhooks: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202) })}}}
	repo := repository{mocks.NewMockTraceRepository(), &mocks.MockMetricsRepository{}}
	router := newRouter(cfg, repo, &mocks.MockWorkspaceStore{}, nil, nil, tokenAuth{token: "user"}, nil, apikey.NewService(nil), nil, opts)
	for _, tt := range []struct {
		path, token string
		want        int
	}{
		{"/v1/extensions/example/identity", "", 401}, {"/v1/extensions/example/identity", "invalid", 401},
		{"/v1/extensions/example/identity", token, 204}, {"/v1/integrations/example", "", 202},
		{"/v1/workspaces", "", 401}, {"/v1/workspaces", token, 200}, {"/v1/health", "", 200},
		{"/v1/traces", "", 401}, {"/v1/traces", "invalid", 401}, {"/v1/traces", token, 200},
	} {
		r := httptest.NewRequest("GET", tt.path, nil)
		if tt.token != "" {
			r.Header.Set("Authorization", "Bearer "+tt.token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != tt.want {
			t.Errorf("%s: %d want %d", tt.path, w.Code, tt.want)
		}
		var body model.ErrorResponse
		if w.Code == 401 && (w.Header().Get("Content-Type") != "application/json" || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Code != "UNAUTHORIZED") {
			t.Errorf("%s: 401 body is not a JSON error: %q %s", tt.path, w.Header().Get("Content-Type"), w.Body.String())
		}
	}
	if err := validateOptions(Options{Extensions: []Extension{{Name: "../auth"}}}); err == nil {
		t.Fatal("invalid route namespace accepted")
	}
}
