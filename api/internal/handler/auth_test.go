package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tracium/api/internal/auth"
	"github.com/tracium/api/internal/middleware"
	"github.com/tracium/api/testing/pgtest"
)

func TestAuthEndpoints(t *testing.T) {
	svc := auth.NewService(auth.NewUserStore(pgtest.NewPool(t)), auth.NewTokenIssuer("test-only-signing-secret"), nil)
	h := NewAuthHandler(svc)
	changePassword := middleware.Auth(svc)(http.HandlerFunc(h.ChangePassword))

	call := func(handler http.Handler, body, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr
	}
	tokenOf := func(rr *httptest.ResponseRecorder) string {
		t.Helper()
		var body tokenResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil || body.Token == "" {
			t.Fatalf("no token in %s", rr.Body.String())
		}
		return body.Token
	}
	expect := func(step string, rr *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		if rr.Code != status {
			t.Fatalf("%s: status = %d, want %d (%s)", step, rr.Code, status, rr.Body.String())
		}
		if code != "" && errorCode(t, rr) != code {
			t.Fatalf("%s: code = %s, want %s", step, errorCode(t, rr), code)
		}
	}

	rr := call(http.HandlerFunc(h.Register), `{"email":"ada@example.com","password":"correct-horse-1"}`, "")
	expect("register", rr, http.StatusCreated, "")
	session := tokenOf(rr)
	expect("register again", call(http.HandlerFunc(h.Register), `{"email":"ADA@example.com","password":"correct-horse-1"}`, ""), http.StatusConflict, "EMAIL_TAKEN")

	expect("login", call(http.HandlerFunc(h.Login), `{"email":"ada@example.com","password":"correct-horse-1"}`, ""), http.StatusOK, "")
	expect("wrong password", call(http.HandlerFunc(h.Login), `{"email":"ada@example.com","password":"wrong-horse-1"}`, ""), http.StatusUnauthorized, "INVALID_CREDENTIALS")
	expect("unknown email", call(http.HandlerFunc(h.Login), `{"email":"bob@example.com","password":"correct-horse-1"}`, ""), http.StatusUnauthorized, "INVALID_CREDENTIALS")

	expect("change without session", call(changePassword, `{"current_password":"correct-horse-1","new_password":"correct-horse-2"}`, ""), http.StatusUnauthorized, "UNAUTHORIZED")
	expect("change with wrong current", call(changePassword, `{"current_password":"wrong-horse-1","new_password":"correct-horse-2"}`, session), http.StatusUnauthorized, "INVALID_CURRENT_PASSWORD")
	expect("change to short password", call(changePassword, `{"current_password":"correct-horse-1","new_password":"short"}`, session), http.StatusBadRequest, "INVALID_PASSWORD_FORMAT")
	rr = call(changePassword, `{"current_password":"correct-horse-1","new_password":"correct-horse-2"}`, session)
	expect("change", rr, http.StatusOK, "")
	renewed := tokenOf(rr)
	expect("old session after change", call(changePassword, `{"current_password":"correct-horse-2","new_password":"correct-horse-3"}`, session), http.StatusUnauthorized, "UNAUTHORIZED")
	expect("old password after change", call(http.HandlerFunc(h.Login), `{"email":"ada@example.com","password":"correct-horse-1"}`, ""), http.StatusUnauthorized, "INVALID_CREDENTIALS")
	expect("renewed session", call(changePassword, `{"current_password":"correct-horse-2","new_password":"correct-horse-3"}`, renewed), http.StatusOK, "")
}
