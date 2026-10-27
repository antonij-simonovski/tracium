package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/tracium/api/internal/apikey"
	"github.com/tracium/api/internal/oauth"
	"github.com/tracium/api/testing/mocks"
)

func TestMCPOAuthFlow(t *testing.T) {
	var cfg Config
	cfg.Server.PublicURL = "https://tracium.example.com"
	cfg.Default()
	keys := apikey.NewService(keyStore{})
	read, _ := keys.CreateRead(t.Context(), "user", "pasted")
	repo := repository{mocks.NewMockTraceRepository(), &mocks.MockMetricsRepository{}}
	svc := oauth.NewService(mocks.NewOAuthStore(), keys, cfg.Server.PublicURL+"/v1/mcp")
	router := newRouter(cfg, repo, &mocks.MockWorkspaceStore{}, nil, nil, tokenAuth{"session": "user"}, nil, keys, nil, Options{}, svc)

	do := func(method, path, token, contentType, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	decode := func(w *httptest.ResponseRecorder, want int) map[string]any {
		t.Helper()
		if w.Code != want {
			t.Fatalf("status %d want %d: %s", w.Code, want, w.Body.String())
		}
		var v map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	ping := `{"jsonrpc":"2.0","id":1,"method":"ping"}`

	w := do("POST", "/v1/mcp", "", "", ping)
	if want := `Bearer resource_metadata="https://tracium.example.com/.well-known/oauth-protected-resource/v1/mcp"`; w.Code != 401 || w.Header().Get("WWW-Authenticate") != want {
		t.Fatalf("unauthenticated mcp: %d %q", w.Code, w.Header().Get("WWW-Authenticate"))
	}
	if w := do("POST", "/v1/mcp", read.Token, "", ping); w.Code != 200 || w.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("pasted read key: %d %q", w.Code, w.Header().Get("WWW-Authenticate"))
	}

	prm := decode(do("GET", "/.well-known/oauth-protected-resource/v1/mcp", "", "", ""), 200)
	if prm["resource"] != "https://tracium.example.com/v1/mcp" || prm["authorization_servers"].([]any)[0] != "https://tracium.example.com" {
		t.Fatalf("resource metadata: %v", prm)
	}
	as := decode(do("GET", "/.well-known/oauth-authorization-server", "", "", ""), 200)
	if as["authorization_endpoint"] != "https://tracium.example.com/oauth/authorize" || as["token_endpoint"] != "https://tracium.example.com/v1/oauth/token" {
		t.Fatalf("server metadata: %v", as)
	}

	const redirect = "http://localhost:4000/callback"
	decode(do("POST", "/v1/oauth/register", "", "application/json", `{"client_name":"Claude Code","redirect_uris":["https://evil.example.com/cb","http://evil.example.com/cb"]}`), 400)
	reg := decode(do("POST", "/v1/oauth/register", "", "application/json", `{"client_name":"Claude Code","redirect_uris":["`+redirect+`"],"token_endpoint_auth_method":"none"}`), 201)
	clientID := reg["client_id"].(string)

	info := "/v1/oauth/authorize?" + url.Values{"client_id": {clientID}, "redirect_uri": {redirect}}.Encode()
	for _, token := range []string{"", read.Token} {
		if w := do("GET", info, token, "", ""); w.Code != 401 {
			t.Fatalf("authorize info with %.8q: %d", token, w.Code)
		}
	}
	if got := decode(do("GET", info, "session", "", ""), 200); got["client_name"] != "Claude Code" {
		t.Fatalf("authorize info: %v", got)
	}
	bad := "/v1/oauth/authorize?" + url.Values{"client_id": {clientID}, "redirect_uri": {"http://localhost:4000/other"}}.Encode()
	decode(do("GET", bad, "session", "", ""), 400)

	verifier := strings.Repeat("x", 64)
	sum := sha256.Sum256([]byte(verifier))
	approve := func(yes bool) url.Values {
		t.Helper()
		body, _ := json.Marshal(map[string]any{
			"client_id": clientID, "redirect_uri": redirect, "response_type": "code", "state": "s1",
			"code_challenge": base64.RawURLEncoding.EncodeToString(sum[:]), "code_challenge_method": "S256",
			"resource": "https://tracium.example.com/v1/mcp", "approve": yes,
		})
		to, err := url.Parse(decode(do("POST", "/v1/oauth/authorize", "session", "application/json", string(body)), 200)["redirect_to"].(string))
		if err != nil || to.Scheme+"://"+to.Host+to.Path != redirect {
			t.Fatalf("redirect_to %v: %v", to, err)
		}
		q := to.Query()
		if q.Get("state") != "s1" || q.Get("iss") != "https://tracium.example.com" {
			t.Fatalf("redirect query: %v", q)
		}
		return q
	}
	if q := approve(false); q.Get("error") != "access_denied" || q.Get("code") != "" {
		t.Fatalf("deny: %v", q)
	}
	code := approve(true).Get("code")

	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID}, "redirect_uri": {redirect}, "code_verifier": {verifier}}
	tok := decode(do("POST", "/v1/oauth/token", "", "application/x-www-form-urlencoded", form.Encode()), 200)
	if tok["token_type"] != "Bearer" {
		t.Fatalf("token: %v", tok)
	}
	if got := decode(do("POST", "/v1/oauth/token", "", "application/x-www-form-urlencoded", form.Encode()), 400); got["error"] != "invalid_grant" {
		t.Fatalf("reused code: %v", got)
	}
	if w := do("POST", "/v1/mcp", tok["access_token"].(string), "", ping); w.Code != 200 {
		t.Fatalf("mcp with oauth token: %d %s", w.Code, w.Body.String())
	}
}

func TestOAuthOffWithoutPublicURL(t *testing.T) {
	var cfg Config
	cfg.Default()
	repo := repository{mocks.NewMockTraceRepository(), &mocks.MockMetricsRepository{}}
	router := newRouter(cfg, repo, &mocks.MockWorkspaceStore{}, nil, nil, tokenAuth{}, nil, apikey.NewService(keyStore{}), nil, Options{}, nil)
	for _, path := range []string{"/.well-known/oauth-authorization-server", "/.well-known/oauth-protected-resource/v1/mcp"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/v1/mcp", strings.NewReader("{}")))
	if w.Code != 401 || w.Header().Get("WWW-Authenticate") != "" {
		t.Errorf("mcp: %d %q", w.Code, w.Header().Get("WWW-Authenticate"))
	}
}

func TestOAuthRateLimitIsSeparateFromLogin(t *testing.T) {
	var cfg Config
	cfg.Server.PublicURL = "https://tracium.example.com"
	cfg.Default()
	cfg.Auth.RateLimitPerMinute = 1
	repo := repository{mocks.NewMockTraceRepository(), &mocks.MockMetricsRepository{}}
	keys := apikey.NewService(keyStore{})
	svc := oauth.NewService(mocks.NewOAuthStore(), keys, cfg.Server.PublicURL+"/v1/mcp")
	router := newRouter(cfg, repo, &mocks.MockWorkspaceStore{}, nil, nil, tokenAuth{}, nil, keys, nil, Options{}, svc)
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("POST", "/v1/oauth/register", strings.NewReader(`{"redirect_uris":["https://claude.ai/cb"]}`)))
		if w.Code != http.StatusCreated {
			t.Fatalf("register %d: %d", i, w.Code)
		}
	}
}
