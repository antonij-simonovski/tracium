package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/tracium/api/internal/apikey"
	"github.com/tracium/api/internal/middleware"
	"github.com/tracium/api/internal/model"
)

// APIKeyHandler manages per-workspace ingestion keys and answers the collector's
// verify calls. Management routes are nested under a workspace and run behind the
// authenticated middleware chain, gated on membership of that workspace; Verify
// is mounted on an unauthenticated (rate-limited) route because its caller is the
// collector, which holds no user session — only the key.
type APIKeyHandler struct {
	svc    *apikey.Service
	access WorkspaceAccess
}

// NewAPIKeyHandler constructs an APIKeyHandler. access gates key management on
// membership of the target workspace — you can only mint or revoke keys for a
// workspace you belong to.
func NewAPIKeyHandler(svc *apikey.Service, access WorkspaceAccess) *APIKeyHandler {
	return &APIKeyHandler{svc: svc, access: access}
}

// maxAPIKeyBody caps the request bodies for key endpoints. They carry at most a
// short name or a single token, so anything larger is rejected before decoding.
const maxAPIKeyBody = 4 << 10 // 4 KiB

// Create handles POST /v1/workspaces/{id}/api-keys — issues a new key bound to
// that workspace. The plaintext token is in the response exactly once and is
// never retrievable again, so the client must capture it here.
func (h *APIKeyHandler) Create(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	if _, ok := resolveWorkspaceScope(w, r, h.access, workspaceID); !ok {
		return
	}
	principal, _ := middleware.PrincipalFromContext(r.Context())

	r.Body = http.MaxBytesReader(w, r.Body, maxAPIKeyBody)
	var body struct {
		Name string `json:"name"`
	}
	// An empty body is allowed — a name is optional and the service defaults it.
	// Only a body that is present but malformed is an error.
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		respondDecodeError(w, err)
		return
	}

	created, err := h.svc.Create(r.Context(), workspaceID, principal.UserID, body.Name)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "could not create api key")
		return
	}
	respondJSON(w, http.StatusCreated, created)
}

// List handles GET /v1/workspaces/{id}/api-keys — the workspace's keys (metadata
// only).
func (h *APIKeyHandler) List(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	if _, ok := resolveWorkspaceScope(w, r, h.access, workspaceID); !ok {
		return
	}

	keys, err := h.svc.List(r.Context(), workspaceID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "could not fetch api keys")
		return
	}
	if keys == nil {
		keys = []model.APIKey{}
	}
	respondJSON(w, http.StatusOK, keys)
}

// Revoke handles DELETE /v1/workspaces/{id}/api-keys/{keyId} — disables a key in
// that workspace.
func (h *APIKeyHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	if _, ok := resolveWorkspaceScope(w, r, h.access, workspaceID); !ok {
		return
	}
	keyID := chi.URLParam(r, "keyId")

	if err := h.svc.Revoke(r.Context(), keyID, workspaceID); err != nil {
		// 404, not 403, when the key is absent or in another workspace — never
		// reveal that a key exists elsewhere.
		if errors.Is(err, apikey.ErrNotFound) {
			respondError(w, http.StatusNotFound, "API_KEY_NOT_FOUND", "api key not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL", "could not revoke api key")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Verify handles POST /v1/ingest/keys/verify — the collector's authenticator
// extension calls this with a presented ingest token and receives the workspace
// the key grants, or 401. It is intentionally terse and leaks no detail about
// why a key failed.
func (h *APIKeyHandler) Verify(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAPIKeyBody)
	var body struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondDecodeError(w, err)
		return
	}
	if body.Key == "" {
		respondError(w, http.StatusUnauthorized, "INVALID_API_KEY", "invalid api key")
		return
	}

	workspaceID, err := h.svc.Verify(r.Context(), body.Key)
	if err != nil {
		if errors.Is(err, apikey.ErrInvalidKey) {
			respondError(w, http.StatusUnauthorized, "INVALID_API_KEY", "invalid api key")
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL", "could not verify api key")
		return
	}
	respondJSON(w, http.StatusOK, struct {
		WorkspaceID string `json:"workspace_id"`
	}{WorkspaceID: workspaceID})
}
