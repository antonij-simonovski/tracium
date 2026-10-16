package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tracium/api/internal/apikey"
	"github.com/tracium/api/internal/model"
)

type fakeAuthenticator struct{}

func (fakeAuthenticator) Authenticate(_ context.Context, token string) (*model.Principal, error) {
	if token == "good" {
		return &model.Principal{UserID: "u-1"}, nil
	}
	if token == "db-down" {
		return nil, fmt.Errorf("%w: connection refused", ErrUnavailable)
	}
	return nil, errors.New("bad token")
}

func TestAuth(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := PrincipalFromContext(r.Context())
		if !ok || p.UserID != "u-1" {
			t.Error("principal missing from context")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	for _, tt := range []struct {
		name, header, code string
		want               int
	}{
		{"missing header", "", "UNAUTHORIZED", http.StatusUnauthorized},
		{"other scheme", "Basic dTpw", "UNAUTHORIZED", http.StatusUnauthorized},
		{"invalid token", "Bearer bad", "UNAUTHORIZED", http.StatusUnauthorized},
		{"backend down", "Bearer db-down", "UNAVAILABLE", http.StatusServiceUnavailable},
		{"valid token", "Bearer good", "", http.StatusNoContent},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			w := httptest.NewRecorder()
			Auth(fakeAuthenticator{})(next).ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d", w.Code, tt.want)
			}
			if tt.code == "" {
				return
			}
			var body model.ErrorResponse
			if w.Header().Get("Content-Type") != "application/json" || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Code != tt.code {
				t.Fatalf("not a JSON error: %q %s", w.Header().Get("Content-Type"), w.Body.String())
			}
		})
	}
}

type fakeKeys struct{}

func (fakeKeys) Lookup(_ context.Context, token string) (*model.APIKey, error) {
	switch token {
	case "trc_good":
		return &model.APIKey{WorkspaceID: "ws-1", CreatedBy: "u-key"}, nil
	case "trc_db-down":
		return nil, errors.New("connection refused")
	}
	return nil, apikey.ErrInvalidKey
}

func TestIngestKeyOr(t *testing.T) {
	for _, tt := range []struct {
		name, header, user, workspace string
		want                          int
	}{
		{"valid key", "Bearer trc_good", "u-key", "ws-1", http.StatusNoContent},
		{"invalid key", "Bearer trc_bad", "", "", http.StatusUnauthorized},
		{"key store down", "Bearer trc_db-down", "", "", http.StatusServiceUnavailable},
		{"session token", "Bearer good", "u-1", "", http.StatusNoContent},
		{"invalid session token", "Bearer bad", "", "", http.StatusUnauthorized},
		{"missing header", "", "", "", http.StatusUnauthorized},
	} {
		t.Run(tt.name, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				p, ok := PrincipalFromContext(r.Context())
				if !ok || p.UserID != tt.user || p.WorkspaceID != tt.workspace {
					t.Errorf("principal = %+v, want user %q workspace %q", p, tt.user, tt.workspace)
				}
				w.WriteHeader(http.StatusNoContent)
			})
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			w := httptest.NewRecorder()
			IngestKeyOr(fakeKeys{}, Auth(fakeAuthenticator{}))(next).ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d", w.Code, tt.want)
			}
		})
	}
}
