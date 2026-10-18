package handler

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/tracium/api/internal/middleware"
)

// WorkspaceAccess resolves which workspaces an authenticated account may read.
// The data handlers depend on this interface (satisfied by the workspace store)
// to enforce the read-side access boundary.
type WorkspaceAccess interface {
	AllowedIDs(ctx context.Context, userID string) ([]string, error)
}

// resolveWorkspaceScope returns the workspaces a request may read: the requested
// one (403 unless the caller is a member), or all of the caller's when requested
// is empty. An empty result matches nothing in the query layer. On failure it
// writes the error response and returns ok=false.
func resolveWorkspaceScope(w http.ResponseWriter, r *http.Request, access WorkspaceAccess, requested string) ([]string, bool) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok || principal.UserID == "" {
		respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user identity could not be resolved")
		return nil, false
	}

	allowed, err := access.AllowedIDs(r.Context(), principal.UserID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "could not resolve workspace access")
		return nil, false
	}

	if requested != "" {
		for _, id := range allowed {
			if id == requested {
				return []string{requested}, true
			}
		}
		respondError(w, http.StatusForbidden, "FORBIDDEN", "you do not have access to that workspace")
		return nil, false
	}
	return allowed, true
}

// OwnerCheck reports whether a user owns a workspace.
type OwnerCheck interface {
	IsOwner(ctx context.Context, workspaceID, userID string) (bool, error)
}

// requireOwner checks the caller owns the {id} workspace, answering 404 (not
// 403) otherwise so its existence isn't revealed.
func requireOwner(w http.ResponseWriter, r *http.Request, owners OwnerCheck) (userID, workspaceID string, ok bool) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok || principal.UserID == "" {
		respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user identity could not be resolved")
		return "", "", false
	}
	workspaceID = chi.URLParam(r, "id")
	owner, err := owners.IsOwner(r.Context(), workspaceID, principal.UserID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "could not verify ownership")
		return "", "", false
	}
	if !owner {
		respondError(w, http.StatusNotFound, "WORKSPACE_NOT_FOUND", "workspace not found")
		return "", "", false
	}
	return principal.UserID, workspaceID, true
}
