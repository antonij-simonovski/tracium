package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tracium/api/extension"
	"github.com/tracium/api/internal/auth"
	"github.com/tracium/api/internal/middleware"
	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/workspace"
)

// UserLookup resolves an account by email, so members can be added by email
// rather than by opaque user id. Satisfied by the auth user store.
type UserLookup interface {
	ByEmail(ctx context.Context, email string) (*model.User, error)
}

const maxWorkspaceBody = 4 << 10

var validEnvs = map[string]bool{"production": true, "staging": true, "development": true}

// WorkspaceHandler handles workspace CRUD and member management for the
// authenticated user.
type WorkspaceHandler struct {
	store        workspace.Store
	users        UserLookup
	entitlements extension.Entitlements
}

// NewWorkspaceHandler constructs a WorkspaceHandler. A nil entitlements allows
// adding any number of members.
func NewWorkspaceHandler(store workspace.Store, users UserLookup, entitlements extension.Entitlements) *WorkspaceHandler {
	return &WorkspaceHandler{store: store, users: users, entitlements: entitlements}
}

// List handles GET /v1/workspaces — returns all workspaces for the current user.
func (h *WorkspaceHandler) List(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok || principal.UserID == "" {
		respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user identity could not be resolved")
		return
	}

	workspaces, err := h.store.List(r.Context(), principal.UserID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "could not fetch workspaces")
		return
	}

	if workspaces == nil {
		workspaces = []model.Workspace{}
	}
	respondJSON(w, http.StatusOK, workspaces)
}

// Create handles POST /v1/workspaces — creates a new workspace for the current user.
func (h *WorkspaceHandler) Create(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok || principal.UserID == "" {
		respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user identity could not be resolved")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxWorkspaceBody)
	var body struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
		Env  string `json:"env"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "request body must be valid JSON")
		return
	}
	if body.Name == "" || body.Slug == "" || body.Env == "" {
		respondError(w, http.StatusBadRequest, "MISSING_FIELDS", "name, slug, and env are required")
		return
	}
	if !validEnvs[body.Env] {
		respondError(w, http.StatusBadRequest, "INVALID_ENV", "env must be production, staging, or development")
		return
	}

	ws := model.Workspace{
		ID:      uuid.NewString(),
		UserID:  principal.UserID,
		Name:    body.Name,
		Slug:    body.Slug,
		Env:     body.Env,
		Role:    workspace.RoleOwner,
		Members: 1,
	}
	if err := h.store.Create(r.Context(), ws); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "could not create workspace")
		return
	}

	respondJSON(w, http.StatusCreated, ws)
}

// Delete handles DELETE /v1/workspaces/{id} — removes a workspace owned by the current user.
func (h *WorkspaceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok || principal.UserID == "" {
		respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user identity could not be resolved")
		return
	}

	id := chi.URLParam(r, "id")
	if err := h.store.Delete(r.Context(), id, principal.UserID); err != nil {
		// 404, not 403, when the workspace belongs to someone else — never
		// reveal that another user's workspace exists.
		if errors.Is(err, workspace.ErrNotFound) {
			respondError(w, http.StatusNotFound, "WORKSPACE_NOT_FOUND", "workspace not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL", "could not delete workspace")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// AddMember handles POST /v1/workspaces/{id}/members — grants another account
// access to the workspace. Owner-only. The member is named by email.
func (h *WorkspaceHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := requireOwner(w, r, h.store)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxWorkspaceBody)
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Email == "" {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "email is required")
		return
	}
	member, err := h.users.ByEmail(r.Context(), auth.NormalizeEmail(body.Email))
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			respondError(w, http.StatusNotFound, "USER_NOT_FOUND", "no account with that email")
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL", "could not look up account")
		return
	}

	// A person whose open invite already holds their seat joins as if accepting
	// it; anyone else takes a new seat, exactly like a new invite.
	allow := func(ctx context.Context, invited bool) error {
		feature := InviteFeature
		if invited {
			feature = MemberAddFeature
		}
		return checkMemberEntitlement(ctx, h.entitlements, extension.Subject{UserID: userID, WorkspaceID: workspaceID}, feature)
	}
	if err := h.store.GrantMember(r.Context(), workspaceID, member.ID, allow); err != nil {
		var denial *entitlementDenial
		if errors.As(err, &denial) {
			denial.respond(w)
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL", "could not add member")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RemoveMember handles DELETE /v1/workspaces/{id}/members/{userId} — revokes an
// account's access. Owner-only; the owner cannot be removed.
func (h *WorkspaceHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := requireOwner(w, r, h.store)
	if !ok {
		return
	}

	if err := h.store.RemoveMember(r.Context(), workspaceID, chi.URLParam(r, "userId")); err != nil {
		if errors.Is(err, workspace.ErrCannotRemoveOwner) {
			respondError(w, http.StatusBadRequest, "CANNOT_REMOVE_OWNER", "the workspace owner cannot be removed")
			return
		}
		if errors.Is(err, workspace.ErrNotFound) {
			respondError(w, http.StatusNotFound, "MEMBER_NOT_FOUND", "member not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL", "could not remove member")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListMembers handles GET /v1/workspaces/{id}/members. Members-only.
func (h *WorkspaceHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	if _, ok := resolveWorkspaceScope(w, r, h.store, workspaceID); !ok {
		return
	}

	members, err := h.store.ListMembers(r.Context(), workspaceID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "could not fetch members")
		return
	}
	if members == nil {
		members = []model.WorkspaceMember{}
	}
	respondJSON(w, http.StatusOK, members)
}
