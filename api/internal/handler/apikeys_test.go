package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/tracium/api/internal/apikey"
	"github.com/tracium/api/internal/middleware"
	"github.com/tracium/api/internal/model"
)

// memKeyStore is an in-memory apikey.Store for driving the handlers without
// Postgres.
type memKeyStore struct {
	byHash map[string]model.APIKey
	hashOf map[string]string
}

func newMemKeyStore() *memKeyStore {
	return &memKeyStore{byHash: map[string]model.APIKey{}, hashOf: map[string]string{}}
}

func (m *memKeyStore) Insert(_ context.Context, key model.APIKey, hash string) (time.Time, error) {
	if key.CreatedAt.IsZero() {
		key.CreatedAt = time.Now().UTC()
	}
	m.byHash[hash] = key
	m.hashOf[key.ID] = hash
	return key.CreatedAt, nil
}
func (m *memKeyStore) List(_ context.Context, workspaceID string) ([]model.APIKey, error) {
	var out []model.APIKey
	for _, k := range m.byHash {
		if k.WorkspaceID == workspaceID {
			out = append(out, k)
		}
	}
	return out, nil
}
func (m *memKeyStore) Revoke(_ context.Context, id, workspaceID string) error {
	hash, ok := m.hashOf[id]
	if !ok {
		return apikey.ErrNotFound
	}
	k := m.byHash[hash]
	if k.WorkspaceID != workspaceID || k.RevokedAt != nil {
		return apikey.ErrNotFound
	}
	now := k.CreatedAt
	k.RevokedAt = &now
	m.byHash[hash] = k
	return nil
}
func (m *memKeyStore) FindActiveByHash(_ context.Context, hash string) (*model.APIKey, error) {
	k, ok := m.byHash[hash]
	if !ok || k.RevokedAt != nil {
		return nil, apikey.ErrNotFound
	}
	return &k, nil
}

// fakeAccess grants membership of a fixed set of workspaces to any user.
type fakeAccess struct{ ids []string }

func (f fakeAccess) AllowedIDs(context.Context, string) ([]string, error) { return f.ids, nil }

// apiKeyTestRouter wires the api-key routes with a fixed principal, as the real
// auth middleware would inject.
func apiKeyTestRouter(h *APIKeyHandler, userID string) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := middleware.ContextWithPrincipal(req.Context(), &model.Principal{UserID: userID})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Get("/v1/workspaces/{id}/api-keys", h.List)
	r.Post("/v1/workspaces/{id}/api-keys", h.Create)
	r.Delete("/v1/workspaces/{id}/api-keys/{keyId}", h.Revoke)
	r.Post("/v1/ingest/keys/verify", h.Verify)
	return r
}

// TestAPIKeyLifecycle exercises the full create → list → verify → revoke →
// verify-fails flow through the real handlers, scoped to a workspace.
func TestAPIKeyLifecycle(t *testing.T) {
	svc := apikey.NewService(newMemKeyStore())
	h := NewAPIKeyHandler(svc, fakeAccess{ids: []string{"ws-1"}})
	router := apiKeyTestRouter(h, "user-1")

	do := func(method, path string, body any) *httptest.ResponseRecorder {
		var r *http.Request
		if body != nil {
			b, _ := json.Marshal(body)
			r = httptest.NewRequest(method, path, bytes.NewReader(b))
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}

	// 1. Create a key in ws-1 — returns the one-time plaintext token.
	w := do(http.MethodPost, "/v1/workspaces/ws-1/api-keys", map[string]string{"name": "ci"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: status %d, body %s", w.Code, w.Body.String())
	}
	var created apikey.Created
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Token == "" || created.Key.ID == "" {
		t.Fatalf("create returned no token/id: %+v", created)
	}
	if created.Key.WorkspaceID != "ws-1" || created.Key.CreatedBy != "user-1" {
		t.Fatalf("key metadata = %+v, want ws-1 / user-1", created.Key)
	}

	// 2. List — shows the key by prefix, never the secret.
	w = do(http.MethodGet, "/v1/workspaces/ws-1/api-keys", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: status %d", w.Code)
	}
	if bytes.Contains(w.Body.Bytes(), []byte(created.Token)) {
		t.Fatal("list response leaked the plaintext token")
	}

	// 3. Verify — resolves the token to the workspace it grants.
	w = do(http.MethodPost, "/v1/ingest/keys/verify", map[string]string{"key": created.Token})
	if w.Code != http.StatusOK {
		t.Fatalf("verify: status %d, body %s", w.Code, w.Body.String())
	}
	var vr struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &vr); err != nil {
		t.Fatalf("decode verify: %v", err)
	}
	if vr.WorkspaceID != "ws-1" {
		t.Errorf("verify workspace_id = %q, want ws-1", vr.WorkspaceID)
	}

	// 4. A bogus token is rejected.
	w = do(http.MethodPost, "/v1/ingest/keys/verify", map[string]string{"key": "trc_not_a_real_key"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("verify bogus: status %d, want 401", w.Code)
	}

	// 5. Revoke, then the same token no longer verifies.
	w = do(http.MethodDelete, "/v1/workspaces/ws-1/api-keys/"+created.Key.ID, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("revoke: status %d", w.Code)
	}
	w = do(http.MethodPost, "/v1/ingest/keys/verify", map[string]string{"key": created.Token})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("verify after revoke: status %d, want 401", w.Code)
	}
}

// TestAPIKeyCreateForbiddenWorkspace: a user who is not a member of the target
// workspace cannot mint a key in it.
func TestAPIKeyCreateForbiddenWorkspace(t *testing.T) {
	svc := apikey.NewService(newMemKeyStore())
	h := NewAPIKeyHandler(svc, fakeAccess{ids: []string{"ws-1"}}) // member of ws-1 only
	router := apiKeyTestRouter(h, "user-1")

	r := httptest.NewRequest(http.MethodPost, "/v1/workspaces/ws-OTHER/api-keys", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("create in non-member workspace: status %d, want 403", w.Code)
	}
}

func TestAPIKeyVerifyRejectsOversizedBody(t *testing.T) {
	h := NewAPIKeyHandler(apikey.NewService(newMemKeyStore()), fakeAccess{})
	body := `{"key":"` + strings.Repeat("a", maxAPIKeyBody) + `"}`
	w := httptest.NewRecorder()
	apiKeyTestRouter(h, "user-1").ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/ingest/keys/verify", strings.NewReader(body)))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413", w.Code)
	}
}
