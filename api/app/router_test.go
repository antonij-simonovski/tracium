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
	"time"
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
	router := newRouter(cfg, repo, &mocks.MockWorkspaceStore{}, nil, nil, tokenAuth{}, nil, apikey.NewService(nil), nil, Options{}, nil)

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
	router := newRouter(cfg, repo, &mocks.MockWorkspaceStore{}, nil, nil, tokenAuth{token: "user"}, nil, apikey.NewService(nil), nil, opts, nil)
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

type keyStore map[string]model.APIKey

func (s keyStore) Insert(_ context.Context, key model.APIKey, hash string) (time.Time, error) {
	s[hash] = key
	return time.Now(), nil
}
func (s keyStore) List(context.Context, string) ([]model.APIKey, error) { return nil, nil }
func (s keyStore) Revoke(context.Context, string, string) error         { return nil }
func (s keyStore) ListRead(context.Context, string) ([]model.APIKey, error) {
	return nil, nil
}
func (s keyStore) RevokeRead(context.Context, string, string) error { return nil }
func (s keyStore) FindActiveByHash(_ context.Context, hash string) (*model.APIKey, error) {
	if key, ok := s[hash]; ok {
		return &key, nil
	}
	return nil, apikey.ErrNotFound
}

func TestIngestKeyReadsOnlySetupChecks(t *testing.T) {
	var cfg Config
	cfg.Default()
	keys := apikey.NewService(keyStore{})
	created, err := keys.Create(context.Background(), "ws-1", "user", "agent")
	if err != nil {
		t.Fatal(err)
	}
	repo := repository{mocks.NewMockTraceRepository(), &mocks.MockMetricsRepository{}}
	router := newRouter(cfg, repo, &mocks.MockWorkspaceStore{}, nil, nil, tokenAuth{"session": "user"}, nil, keys, nil, Options{}, nil)
	for _, tt := range []struct {
		path, token string
		want        int
	}{
		{"/v1/metrics/setup-checks", created.Token, 200},
		{"/v1/metrics/setup-checks?workspace_id=ws-1", created.Token, 200},
		{"/v1/metrics/setup-checks?workspace_id=ws-2", created.Token, 403},
		{"/v1/metrics/setup-checks", "trc_unknown", 401},
		{"/v1/metrics/setup-checks", "session", 200},
		{"/v1/metrics/kpis", created.Token, 401},
		{"/v1/traces", created.Token, 401},
		{"/v1/workspaces", created.Token, 401},
	} {
		r := httptest.NewRequest("GET", tt.path, nil)
		r.Header.Set("Authorization", "Bearer "+tt.token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != tt.want {
			t.Errorf("%s with %.8s: %d want %d", tt.path, tt.token, w.Code, tt.want)
		}
	}
}

type denyReads struct{ user string }

func (d denyReads) Check(_ context.Context, s extension.Subject, feature string) (extension.Decision, error) {
	return extension.Decision{Allowed: feature != "telemetry.read" || s.UserID != d.user}, nil
}

func TestIngestKeyFollowsCreatorEntitlements(t *testing.T) {
	var cfg Config
	cfg.Default()
	keys := apikey.NewService(keyStore{})
	lapsed, _ := keys.Create(context.Background(), "ws-1", "lapsed", "agent")
	paying, _ := keys.Create(context.Background(), "ws-1", "paying", "agent")
	repo := repository{mocks.NewMockTraceRepository(), &mocks.MockMetricsRepository{}}
	router := newRouter(cfg, repo, &mocks.MockWorkspaceStore{}, nil, nil, tokenAuth{}, nil, keys, nil, Options{Entitlements: denyReads{"lapsed"}}, nil)
	for token, want := range map[string]int{lapsed.Token: 403, paying.Token: 200} {
		r := httptest.NewRequest("GET", "/v1/metrics/setup-checks", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("%.8s: %d want %d", token, w.Code, want)
		}
	}
}

func TestReadKeysActAsTheirOwner(t *testing.T) {
	var cfg Config
	cfg.Default()
	keys := apikey.NewService(keyStore{})
	read, _ := keys.CreateRead(context.Background(), "user", "mcp")
	ingest, _ := keys.Create(context.Background(), "ws-1", "user", "app")
	repo := repository{mocks.NewMockTraceRepository(), &mocks.MockMetricsRepository{}}
	workspaces := &mocks.MockWorkspaceStore{Workspaces: []model.Workspace{{ID: "ws-1", UserID: "user"}, {ID: "ws-2", UserID: "user"}, {ID: "ws-3", UserID: "someone"}}}
	router := newRouter(cfg, repo, workspaces, nil, nil, tokenAuth{"session": "user"}, nil, keys, nil, Options{}, nil)
	query := `{"metrics":["cost"],"workspace_id":"ws-2"}`
	ping := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	for _, tt := range []struct {
		method, path, token, body string
		want                      int
	}{
		{"POST", "/v1/query", "session", `{"metrics":["cost"]}`, 200},
		{"POST", "/v1/query", ingest.Token, query, 403},
		{"POST", "/v1/query", "trc_unknown", query, 401},
		{"POST", "/v1/query", read.Token, `{"metrics":["price"]}`, 400},
		{"POST", "/v1/query", read.Token, `{"metrics":["cost"],"workspace_id":"ws-3"}`, 403},
		{"GET", "/v1/query/schema?workspace_id=ws-1", read.Token, "", 200},
		{"POST", "/v1/mcp", read.Token, ping, 200},
		{"POST", "/v1/mcp", ingest.Token, ping, 403},
		{"POST", "/v1/mcp", "", ping, 401},
		{"POST", "/v1/mcp/" + read.Token, "", ping, 200},
		{"POST", "/v1/mcp/" + ingest.Token, "", ping, 403},
		{"POST", "/v1/mcp/trc_unknown", "", ping, 401},
		{"POST", "/v1/mcp/session", "", ping, 401},
		{"GET", "/v1/metrics/setup-checks", read.Token, "", 200},
		{"GET", "/v1/metrics/kpis", read.Token, "", 401},
		{"GET", "/v1/traces", read.Token, "", 401},
		{"GET", "/v1/account/api-keys", read.Token, "", 401},
		{"POST", "/v1/account/api-keys", read.Token, `{}`, 401},
		{"GET", "/v1/account/api-keys", "session", "", 200},
		{"POST", "/v1/query", read.Token, query, 200},
	} {
		r := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
		if tt.token != "" {
			r.Header.Set("Authorization", "Bearer "+tt.token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != tt.want {
			t.Errorf("%s %s with %.8s: %d want %d: %s", tt.method, tt.path, tt.token, w.Code, tt.want, w.Body.String())
		}
	}
	// The read key's query is the last to reach the repository.
	if got := repo.MockMetricsRepository.LastQuery.WorkspaceIDs; len(got) != 1 || got[0] != "ws-2" {
		t.Fatalf("read key query scoped to %v, want [ws-2]", got)
	}
}

func TestQueryAndMCPShareOneRateLimitPerAccount(t *testing.T) {
	var cfg Config
	cfg.Default()
	cfg.Auth.QueryRateLimitPerMinute = 2
	repo := repository{mocks.NewMockTraceRepository(), &mocks.MockMetricsRepository{}}
	auth := tokenAuth{"a": "u1", "b": "u2"}
	router := newRouter(cfg, repo, &mocks.MockWorkspaceStore{}, nil, nil, auth, nil, apikey.NewService(keyStore{}), nil, Options{}, nil)
	for i, tt := range []struct{ path, token string }{
		{"/v1/mcp", "a"}, {"/v1/mcp/trc_unknown", "a"}, {"/v1/query", "a"}, {"/v1/query", "b"},
	} {
		r := httptest.NewRequest("POST", tt.path, strings.NewReader("{}"))
		r.Header.Set("Authorization", "Bearer "+tt.token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if throttled := w.Code == http.StatusTooManyRequests; throttled != (i == 2) {
			t.Fatalf("%s as %s: status %d", tt.path, tt.token, w.Code)
		}
	}
}
