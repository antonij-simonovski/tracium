package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/tracium/api/internal/middleware"
	"github.com/tracium/api/testing/pgtest"
)

const testSecret = "test-only-signing-secret"

func newTestService(t *testing.T) *Service {
	return NewService(NewUserStore(pgtest.NewPool(t)), NewTokenIssuer(testSecret), nil)
}

func TestAuthenticateResolvesAccount(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	reg, err := s.Register(ctx, "Ada@Example.com", "correct-horse-1")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Authenticate(ctx, reg.Token)
	if err != nil {
		t.Fatal(err)
	}
	user, err := s.users.ByEmail(ctx, "ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if p.UserID != user.ID || p.TenantID != user.TenantID || p.Role != "admin" {
		t.Fatalf("principal = %+v, want account %+v", p, user)
	}
}

func TestChangePasswordEndsExistingSessions(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	reg, err := s.Register(ctx, "ada@example.com", "correct-horse-1")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Authenticate(ctx, reg.Token)
	if err != nil {
		t.Fatal(err)
	}

	renewed, err := s.ChangePassword(ctx, p.UserID, "correct-horse-1", "correct-horse-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, reg.Token); err == nil {
		t.Fatal("token issued before the password change still authenticates")
	}
	if _, err := s.Authenticate(ctx, renewed); err != nil {
		t.Fatalf("token returned by the change: %v", err)
	}
	if _, err := s.Login(ctx, "ada@example.com", "correct-horse-1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password: err = %v", err)
	}
	token, err := s.Login(ctx, "ada@example.com", "correct-horse-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, token); err != nil {
		t.Fatalf("new session: %v", err)
	}
}

func TestAuthenticateRejectsBadTokens(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	reg, err := s.Register(ctx, "ada@example.com", "correct-horse-1")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Authenticate(ctx, reg.Token)
	if err != nil {
		t.Fatal(err)
	}
	sign := func(method jwt.SigningMethod, secret string, claims jwt.MapClaims) string {
		token, err := jwt.NewWithClaims(method, claims).SignedString([]byte(secret))
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	exp := time.Now().Add(time.Hour).Unix()

	for name, token := range map[string]string{
		"other secret":   sign(jwt.SigningMethodHS256, "another-secret", jwt.MapClaims{"sub": p.UserID, "exp": exp}),
		"other method":   sign(jwt.SigningMethodHS512, testSecret, jwt.MapClaims{"sub": p.UserID, "exp": exp}),
		"expired":        sign(jwt.SigningMethodHS256, testSecret, jwt.MapClaims{"sub": p.UserID, "exp": time.Now().Add(-time.Minute).Unix()}),
		"no expiry":      sign(jwt.SigningMethodHS256, testSecret, jwt.MapClaims{"sub": p.UserID}),
		"unknown user":   sign(jwt.SigningMethodHS256, testSecret, jwt.MapClaims{"sub": "00000000-0000-0000-0000-000000000000", "exp": exp}),
		"wrong password": sign(jwt.SigningMethodHS256, testSecret, jwt.MapClaims{"sub": p.UserID, "pwd": "stale", "exp": exp}),
	} {
		if _, err := s.Authenticate(ctx, token); err == nil || errors.Is(err, middleware.ErrUnavailable) {
			t.Errorf("%s: err = %v, want a rejection", name, err)
		}
	}

	legacy := sign(jwt.SigningMethodHS256, testSecret, jwt.MapClaims{"sub": p.UserID, "tenant_id": p.TenantID, "role": p.Role, "exp": exp})
	if _, err := s.Authenticate(ctx, legacy); err != nil {
		t.Fatalf("token issued before password stamps: %v", err)
	}
}

func TestLoginUnknownEmail(t *testing.T) {
	s := newTestService(t)
	if _, err := s.Login(context.Background(), "nobody@example.com", "correct-horse-1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestAuthenticateReportsStoreOutageAsUnavailable(t *testing.T) {
	pool := pgtest.NewPool(t)
	s := NewService(NewUserStore(pool), NewTokenIssuer(testSecret), nil)
	ctx := context.Background()
	reg, err := s.Register(ctx, "ada@example.com", "correct-horse-1")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	if _, err := s.Authenticate(ctx, reg.Token); !errors.Is(err, middleware.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}
