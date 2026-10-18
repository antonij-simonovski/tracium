package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/tracium/api/internal/middleware"
	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/testing/mocks"
)

// stubUserLookup satisfies handler.UserLookup for tests that don't exercise
// member-by-email resolution.
type stubUserLookup struct{}

func (stubUserLookup) ByEmail(context.Context, string) (*model.User, error) {
	return &model.User{ID: "user-b"}, nil
}

// stubAuthenticator authenticates every request as the given user, so the
// handler under test sees a Principal the way the middleware chain provides it.
type stubAuthenticator struct{ userID string }

func (a stubAuthenticator) Authenticate(context.Context, string) (*model.Principal, error) {
	return &model.Principal{UserID: a.userID, TenantID: "tenant-test", Role: "user"}, nil
}

// serve runs handler as userID, or without auth when userID is empty, with the
// given chi URL params.
func serve(handler http.HandlerFunc, method, body, userID string, params map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	if userID == "" {
		handler.ServeHTTP(rr, req)
		return rr
	}
	req.Header.Set("Authorization", "Bearer test-token")
	middleware.Auth(stubAuthenticator{userID: userID})(handler).ServeHTTP(rr, req)
	return rr
}

func errorCode(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var body model.ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v (%s)", err, rr.Body.String())
	}
	return body.Code
}

func TestWorkspaceDelete(t *testing.T) {
	store := &mocks.MockWorkspaceStore{
		Workspaces: []model.Workspace{mocks.NewTestWorkspace("ws-1", "user-a")},
	}
	h := NewWorkspaceHandler(store, stubUserLookup{}, nil)

	rr := serve(h.Delete, http.MethodDelete, "", "user-a", map[string]string{"id": "ws-1"})

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rr.Code)
	}
	if len(store.Workspaces) != 0 {
		t.Errorf("workspace was not removed: %+v", store.Workspaces)
	}
}

func TestWorkspaceDeleteOtherUsersWorkspace(t *testing.T) {
	store := &mocks.MockWorkspaceStore{
		Workspaces: []model.Workspace{mocks.NewTestWorkspace("ws-1", "user-a")},
	}
	h := NewWorkspaceHandler(store, stubUserLookup{}, nil)

	rr := serve(h.Delete, http.MethodDelete, "", "user-b", map[string]string{"id": "ws-1"})

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
	if code := errorCode(t, rr); code != "WORKSPACE_NOT_FOUND" {
		t.Errorf("code = %q, want WORKSPACE_NOT_FOUND", code)
	}
	if len(store.Workspaces) != 1 {
		t.Errorf("owner's workspace was removed: %+v", store.Workspaces)
	}
}

func TestWorkspaceCreateReturnsLowercaseOwnerRole(t *testing.T) {
	h := NewWorkspaceHandler(&mocks.MockWorkspaceStore{}, stubUserLookup{}, nil)

	rr := serve(h.Create, http.MethodPost, `{"name":"Prod","slug":"prod","env":"production"}`, "user-a", nil)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rr.Code, rr.Body.String())
	}
	var ws model.Workspace
	if err := json.Unmarshal(rr.Body.Bytes(), &ws); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ws.Role != "owner" {
		t.Errorf("role = %q, want owner", ws.Role)
	}
}

func TestWorkspaceCreateRejectsUnknownEnv(t *testing.T) {
	h := NewWorkspaceHandler(&mocks.MockWorkspaceStore{}, stubUserLookup{}, nil)

	rr := serve(h.Create, http.MethodPost, `{"name":"Prod","slug":"prod","env":"qa"}`, "user-a", nil)

	if rr.Code != http.StatusBadRequest || errorCode(t, rr) != "INVALID_ENV" {
		t.Fatalf("status = %d, body = %s; want 400 INVALID_ENV", rr.Code, rr.Body.String())
	}
}
