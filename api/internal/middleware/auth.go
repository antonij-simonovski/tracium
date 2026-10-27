package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"net/http"
	"slices"
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

// KeyLookup resolves a workspace api key to the active key.
type KeyLookup interface {
	Lookup(ctx context.Context, token string) (*model.APIKey, error)
}

// APIKeyOr authenticates a bearer api key whose scope is one of scopes as its
// creator, limited to the key's workspace when it has one (ingest keys), and
// hands every other request to fallback.
func APIKeyOr(keys KeyLookup, fallback func(http.Handler) http.Handler, scopes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		other := fallback(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || !strings.HasPrefix(token, apikey.TokenPrefix) {
				other.ServeHTTP(w, r)
				return
			}
			key, err := keys.Lookup(r.Context(), token)
			if errors.Is(err, apikey.ErrInvalidKey) {
				WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid api key")
				return
			}
			if err != nil {
				WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "could not verify the api key")
				return
			}
			if !slices.Contains(scopes, key.Scope) {
				WriteError(w, http.StatusForbidden, "FORBIDDEN", "this api key's scope cannot access this endpoint")
				return
			}
			ctx := context.WithValue(r.Context(), principalKey, &model.Principal{UserID: key.CreatedBy, WorkspaceID: key.WorkspaceID})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// BearerFromPath lets a client that can only be given a URL, such as an MCP
// connector, carry its key in the path parameter param. A header still wins.
func BearerFromPath(param string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := chi.URLParam(r, param)
			// Only api keys belong in a URL; never a session token.
			if !strings.HasPrefix(key, apikey.TokenPrefix) {
				WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid api key")
				return
			}
			if r.Header.Get("Authorization") == "" {
				r.Header.Set("Authorization", "Bearer "+key)
			}
			next.ServeHTTP(w, r)
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

// Challenge adds a WWW-Authenticate header with value to every 401, so a client
// learns where to get a token (RFC 9728).
func Challenge(value string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(challengeWriter{w, value}, r)
		})
	}
}

type challengeWriter struct {
	http.ResponseWriter
	value string
}

func (c challengeWriter) WriteHeader(status int) {
	if status == http.StatusUnauthorized {
		c.Header().Set("WWW-Authenticate", c.value)
	}
	c.ResponseWriter.WriteHeader(status)
}
