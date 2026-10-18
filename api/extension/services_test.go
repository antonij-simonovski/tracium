package extension

import (
	"context"
	"errors"
	"github.com/tracium/api/internal/middleware"
	"net/http"
	"net/http/httptest"
	"testing"
)

type access struct {
	ids []string
	err error
}

func (a access) AllowedIDs(context.Context, string) ([]string, error) { return a.ids, a.err }

type entitlement struct {
	called  *bool
	allowed bool
	err     error
}

func (e entitlement) Check(context.Context, Subject, string) (Decision, error) {
	*e.called = true
	return Decision{Allowed: e.allowed}, e.err
}
func TestFeatureChecksMembershipFirst(t *testing.T) {
	for _, tt := range []struct {
		name        string
		ids         []string
		allowed     bool
		providerErr error
		want        int
		called      bool
	}{
		{"foreign workspace", []string{"other"}, true, nil, 403, false},
		{"unavailable feature", []string{"target"}, false, nil, 403, true},
		{"provider failure", []string{"target"}, true, errors.New("offline"), 503, true},
		{"allowed", []string{"target"}, true, nil, 204, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			s := Services{Workspaces: access{ids: tt.ids}, Entitlements: entitlement{&called, tt.allowed, tt.providerErr}}
			h := s.RequireFeature("notifications.email")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
			r := httptest.NewRequest("GET", "/?workspace_id=target", nil)
			r = r.WithContext(middleware.ContextWithPrincipal(r.Context(), &Principal{UserID: "u"}))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.want || called != tt.called {
				t.Fatalf("status=%d called=%v", w.Code, called)
			}
			if w.Code != 204 && w.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("error body is %q, want JSON", w.Header().Get("Content-Type"))
			}
		})
	}
}
func TestGateConsultsEntitlementsForCaller(t *testing.T) {
	for _, tt := range []struct {
		name        string
		entitlement *entitlement
		want        int
		called      bool
	}{
		{"no provider allows", nil, 204, false},
		{"denied", &entitlement{allowed: false}, 403, true},
		{"provider failure", &entitlement{allowed: true, err: errors.New("offline")}, 503, true},
		{"allowed", &entitlement{allowed: true}, 204, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			s := Services{}
			if tt.entitlement != nil {
				tt.entitlement.called = &called
				s.Entitlements = *tt.entitlement
			}
			h := s.Gate("workspaces.create")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
			r := httptest.NewRequest("POST", "/", nil)
			r = r.WithContext(middleware.ContextWithPrincipal(r.Context(), &Principal{UserID: "u"}))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.want || called != tt.called {
				t.Fatalf("status=%d called=%v", w.Code, called)
			}
			if w.Code != 204 && w.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("error body is %q, want JSON", w.Header().Get("Content-Type"))
			}
		})
	}
}
func TestDefaultsDoNotGrantUnknownFeatures(t *testing.T) {
	d, err := (CoreEntitlements{}).Check(context.Background(), Subject{}, "notifications.email")
	if err != nil || d.Allowed {
		t.Fatal("unknown feature allowed")
	}
	if !errors.Is((DisabledMailer{}).Send(context.Background(), Message{}), ErrMailDisabled) {
		t.Fatal("disabled mail reported success")
	}
}
