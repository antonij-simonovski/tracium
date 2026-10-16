package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/tracium/api/internal/apikey"
	"github.com/tracium/api/internal/model"
)

// Authenticator validates a bearer token and returns the authenticated Principal.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (*model.Principal, error)
}

// ErrUnavailable marks an Authenticate failure that says nothing about the
// token, such as a database outage. Auth answers it with 503, not 401.
var ErrUnavailable = errors.New("authentication unavailable")

type contextKey int

const principalKey contextKey = iota

// Auth returns a middleware that validates the Authorization: Bearer <token> header
// and injects the resulting Principal into the request context.
func Auth(authenticator Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing authorization header")
				return
			}

			const prefix = "Bearer "
			if !strings.HasPrefix(header, prefix) {
				WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authorization header must use Bearer scheme")
				return
			}

			token := strings.TrimPrefix(header, prefix)
			principal, err := authenticator.Authenticate(r.Context(), token)
			if errors.Is(err, ErrUnavailable) {
				WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "could not verify the session")
				return
			}
			if err != nil {
				WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid or expired token")
				return
			}

			ctx := context.WithValue(r.Context(), principalKey, principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// KeyVerifier resolves a workspace ingest key to the workspace it grants.
type KeyVerifier interface {
	Verify(ctx context.Context, token string) (string, error)
}

// IngestKeyOr authenticates a bearer ingest key as a principal scoped to the
// key's workspace and hands every other request to fallback.
func IngestKeyOr(keys KeyVerifier, fallback func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		other := fallback(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || !strings.HasPrefix(token, apikey.TokenPrefix) {
				other.ServeHTTP(w, r)
				return
			}
			workspaceID, err := keys.Verify(r.Context(), token)
			if errors.Is(err, apikey.ErrInvalidKey) {
				WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid api key")
				return
			}
			if err != nil {
				WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "could not verify the api key")
				return
			}
			ctx := context.WithValue(r.Context(), principalKey, &model.Principal{WorkspaceID: workspaceID})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// WriteError writes the standard JSON ErrorResponse with the given status.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(model.ErrorResponse{Code: code, Message: message})
}

// PrincipalFromContext retrieves the authenticated Principal from the request context.
// Handlers can assume this is always set — the auth middleware ensures it.
func PrincipalFromContext(ctx context.Context) (*model.Principal, bool) {
	p, ok := ctx.Value(principalKey).(*model.Principal)
	return p, ok
}

// ContextWithPrincipal attaches a Principal to ctx exactly as the Auth
// middleware would. Exported so tests can exercise handlers that require an
// authenticated principal without wiring the full middleware chain.
func ContextWithPrincipal(ctx context.Context, p *model.Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}
