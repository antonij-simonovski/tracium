package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/tracium/api/extension"
	"github.com/tracium/api/internal/auth"
	"github.com/tracium/api/internal/middleware"
)

// AuthHandler handles account registration and login.
type AuthHandler struct {
	svc *auth.Service
}

// NewAuthHandler constructs an AuthHandler with the given auth service.
func NewAuthHandler(svc *auth.Service) *AuthHandler {
	return &AuthHandler{svc: svc}
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type tokenResponse struct {
	Token string `json:"token"`
}

type confirmationResponse struct {
	ConfirmationRequired bool `json:"confirmation_required"`
}

// Register handles POST /v1/auth/register.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	creds, ok := decodeCredentials(w, r)
	if !ok {
		return
	}

	result, err := h.svc.Register(r.Context(), creds.Email, creds.Password)
	if err != nil {
		if errors.Is(err, auth.ErrEmailTaken) {
			respondError(w, http.StatusConflict, "EMAIL_TAKEN", "An account with that email already exists")
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL", "Could not create account")
		return
	}

	// When email confirmation is required the account exists but has no session
	// yet; report 202 without a token so the client prompts the user to confirm.
	if result.ConfirmationRequired {
		respondJSON(w, http.StatusAccepted, confirmationResponse{ConfirmationRequired: true})
		return
	}

	respondJSON(w, http.StatusCreated, tokenResponse{Token: result.Token})
}

// Login handles POST /v1/auth/login.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	creds, ok := decodeCredentials(w, r)
	if !ok {
		return
	}

	token, err := h.svc.Login(r.Context(), creds.Email, creds.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			respondError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Invalid email or password")
			return
		}
		if errors.Is(err, extension.ErrEmailUnverified) {
			respondError(w, http.StatusForbidden, "EMAIL_UNVERIFIED", "Confirm your email address before signing in")
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL", "Could not sign in")
		return
	}

	respondJSON(w, http.StatusOK, tokenResponse{Token: token})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// ChangePassword handles POST /v1/auth/password for the signed-in user.
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok || principal.UserID == "" {
		respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user identity could not be resolved")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxCredentialBody)
	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "Request body must be valid JSON")
		return
	}
	if req.CurrentPassword == "" || req.NewPassword == "" {
		respondError(w, http.StatusBadRequest, "MISSING_FIELDS", "Current and new password are required")
		return
	}

	token, err := h.svc.ChangePassword(r.Context(), principal.UserID, req.CurrentPassword, req.NewPassword)
	switch {
	case err == nil:
		respondJSON(w, http.StatusOK, tokenResponse{Token: token})
	case errors.Is(err, auth.ErrInvalidCurrentPassword):
		respondError(w, http.StatusUnauthorized, "INVALID_CURRENT_PASSWORD", "Current password is incorrect")
	case errors.Is(err, auth.ErrNoPassword):
		respondError(w, http.StatusConflict, "NO_PASSWORD", "This account doesn't have a password yet. Sign out and use password reset to set one.")
	case errors.Is(err, auth.ErrPasswordTooShort), errors.Is(err, auth.ErrPasswordTooLong):
		respondError(w, http.StatusBadRequest, "INVALID_PASSWORD_FORMAT", err.Error())
	default:
		respondError(w, http.StatusInternalServerError, "INTERNAL", "Could not change password")
	}
}

// maxCredentialBody caps the request body for auth endpoints. Credentials are
// tiny; anything larger is rejected before it is buffered or decoded so an
// oversized payload cannot be used to exhaust memory or reach bcrypt.
const maxCredentialBody = 4 << 10 // 4 KiB

func decodeCredentials(w http.ResponseWriter, r *http.Request) (credentials, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxCredentialBody)

	var creds credentials
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			respondError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "Request body is too large")
			return creds, false
		}
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "Request body must be valid JSON")
		return creds, false
	}

	creds.Email = strings.TrimSpace(creds.Email)
	if creds.Email == "" || creds.Password == "" {
		respondError(w, http.StatusBadRequest, "MISSING_FIELDS", "Email and password are required")
		return creds, false
	}

	if err := auth.ValidateCredentials(creds.Email, creds.Password); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_CREDENTIALS_FORMAT", err.Error())
		return creds, false
	}

	return creds, true
}
