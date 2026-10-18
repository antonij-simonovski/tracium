package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tracium/api/extension"
	"github.com/tracium/api/internal/auth"
	"github.com/tracium/api/internal/middleware"
	"github.com/tracium/api/internal/model"
	tokens "github.com/tracium/api/internal/token"
	"github.com/tracium/api/internal/workspace"
)

// InviteHandler manages workspace invitations.
type InviteHandler struct {
	invites      workspace.InviteStore
	owners       OwnerCheck
	entitlements extension.Entitlements
	notifier     extension.InviteNotifier
}

// NewInviteHandler constructs an InviteHandler. A nil entitlements allows all
// invites, and a nil notifier sends nothing.
func NewInviteHandler(invites workspace.InviteStore, owners OwnerCheck, entitlements extension.Entitlements, notifier extension.InviteNotifier) *InviteHandler {
	return &InviteHandler{invites: invites, owners: owners, entitlements: entitlements, notifier: notifier}
}

const maxInviteBody = 4 << 10 // 4 KiB

// Create handles POST /v1/workspaces/{id}/invites. Owner-only.
func (h *InviteHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := requireOwner(w, r, h.owners)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxInviteBody)
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "request body must be valid JSON")
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	if email == "" {
		respondError(w, http.StatusBadRequest, "MISSING_FIELDS", "email is required")
		return
	}
	if err := auth.ValidateEmail(email); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_EMAIL", err.Error())
		return
	}

	token, err := tokens.New(workspace.InviteTokenPrefix)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "could not create invite")
		return
	}
	inv := model.WorkspaceInvite{
		ID:          uuid.NewString(),
		WorkspaceID: workspaceID,
		Email:       email,
		Role:        workspace.RoleMember,
		InvitedBy:   userID,
		ExpiresAt:   time.Now().Add(workspace.InviteTTL).UTC(),
	}
	// The store runs this only for a new invitation: replacing an open invite's
	// link takes no new seat, which keeps "New link" working in a full workspace.
	allow := func(ctx context.Context) error {
		return checkMemberEntitlement(ctx, h.entitlements, extension.Subject{UserID: userID, WorkspaceID: workspaceID}, InviteFeature)
	}
	if err := h.invites.CreateInvite(r.Context(), &inv, tokens.Hash(token), allow); err != nil {
		var denial *entitlementDenial
		if errors.As(err, &denial) {
			denial.respond(w)
			return
		}
		if errors.Is(err, workspace.ErrAlreadyMember) {
			respondError(w, http.StatusConflict, "ALREADY_MEMBER", "that account is already a member of this workspace")
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL", "could not create invite")
		return
	}
	respondJSON(w, http.StatusCreated, model.CreatedInvite{WorkspaceInvite: inv, Token: token, EmailSent: h.notify(r.Context(), inv, token)})
}

// notify hands a new invite to the notifier and reports whether it was sent. A
// failure is logged, not returned: the invite exists and the owner can still
// share the link.
func (h *InviteHandler) notify(ctx context.Context, inv model.WorkspaceInvite, token string) bool {
	if h.notifier == nil {
		return false
	}
	preview, err := h.invites.PreviewInvite(ctx, tokens.Hash(token))
	if err != nil {
		log.Printf("invite %s: load details for notification: %v", inv.ID, err)
		return false
	}
	if err := h.notifier.InviteCreated(ctx, extension.Invite{
		ID:             inv.ID,
		WorkspaceID:    inv.WorkspaceID,
		WorkspaceName:  preview.WorkspaceName,
		Email:          inv.Email,
		InvitedByEmail: preview.InvitedByEmail,
		Token:          token,
		ExpiresAt:      inv.ExpiresAt,
	}); err != nil {
		log.Printf("invite %s: notify: %v", inv.ID, err)
		return false
	}
	return true
}

// List handles GET /v1/workspaces/{id}/invites. Owner-only.
func (h *InviteHandler) List(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := requireOwner(w, r, h.owners)
	if !ok {
		return
	}
	invites, err := h.invites.ListInvites(r.Context(), workspaceID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "could not fetch invites")
		return
	}
	if invites == nil {
		invites = []model.WorkspaceInvite{}
	}
	respondJSON(w, http.StatusOK, invites)
}

// Revoke handles DELETE /v1/workspaces/{id}/invites/{inviteId}. Owner-only.
func (h *InviteHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := requireOwner(w, r, h.owners)
	if !ok {
		return
	}
	if err := h.invites.RevokeInvite(r.Context(), workspaceID, chi.URLParam(r, "inviteId")); err != nil {
		if errors.Is(err, workspace.ErrInviteNotFound) {
			respondError(w, http.StatusNotFound, "INVITE_NOT_FOUND", "invite not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL", "could not revoke invite")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Preview handles GET /v1/invites/{token}. It needs no session.
func (h *InviteHandler) Preview(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if !tokens.Valid(workspace.InviteTokenPrefix, token) {
		respondError(w, http.StatusNotFound, "INVITE_NOT_FOUND", "invite not found")
		return
	}
	preview, err := h.invites.PreviewInvite(r.Context(), tokens.Hash(token))
	if err != nil {
		respondInviteError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, preview)
}

// Accept handles POST /v1/invites/{token}/accept.
func (h *InviteHandler) Accept(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok || principal.UserID == "" {
		respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user identity could not be resolved")
		return
	}
	token := chi.URLParam(r, "token")
	if !tokens.Valid(workspace.InviteTokenPrefix, token) {
		respondError(w, http.StatusNotFound, "INVITE_NOT_FOUND", "invite not found")
		return
	}

	// The seat check runs inside the accept transaction, once the invite is known
	// to be open and addressed to this account.
	allow := func(ctx context.Context, workspaceID string) error {
		return checkMemberEntitlement(ctx, h.entitlements, extension.Subject{UserID: principal.UserID, WorkspaceID: workspaceID}, MemberAddFeature)
	}
	workspaceID, err := h.invites.AcceptInvite(r.Context(), tokens.Hash(token), principal.UserID, allow)
	if err != nil {
		respondInviteError(w, err)
		return
	}
	respondJSON(w, http.StatusOK, struct {
		WorkspaceID string `json:"workspace_id"`
	}{workspaceID})
}

func respondInviteError(w http.ResponseWriter, err error) {
	var denial *entitlementDenial
	switch {
	case errors.As(err, &denial):
		denial.respond(w)
	case errors.Is(err, workspace.ErrInviteNotFound):
		respondError(w, http.StatusNotFound, "INVITE_NOT_FOUND", "invite not found")
	case errors.Is(err, workspace.ErrInviteClosed):
		respondError(w, http.StatusGone, "INVITE_EXPIRED", "this invite has expired or is no longer valid")
	case errors.Is(err, workspace.ErrInviteEmailMismatch):
		respondError(w, http.StatusForbidden, "INVITE_EMAIL_MISMATCH", "this invite was sent to a different email address")
	default:
		respondError(w, http.StatusInternalServerError, "INTERNAL", "could not process invite")
	}
}
