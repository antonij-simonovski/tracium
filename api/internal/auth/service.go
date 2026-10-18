package auth

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/tracium/api/extension"
	"github.com/tracium/api/internal/middleware"
	"github.com/tracium/api/internal/model"
)

// ErrInvalidCredentials is returned when an email/password pair does not match.
var ErrInvalidCredentials = errors.New("invalid credentials")

// ErrInvalidCurrentPassword is returned when ChangePassword's currentPassword
// does not match the account's stored hash.
var ErrInvalidCurrentPassword = errors.New("current password is incorrect")

// ErrNoPassword is returned by ChangePassword for an account that has no
// password, such as one an embedding application created for an external
// identity provider. It can only get one through that application's reset flow.
var ErrNoPassword = errors.New("account has no password")

// Service ties account storage and token issuance together.
type Service struct {
	users     *UserStore
	tokens    *TokenIssuer
	lifecycle extension.AccountLifecycle
}

// NewService constructs the auth service. lifecycle is optional: when nil,
// registration issues a session token immediately and login is not gated on
// email confirmation. An embedding application (the hosted service) passes a
// lifecycle to require confirmation.
func NewService(users *UserStore, tokens *TokenIssuer, lifecycle extension.AccountLifecycle) *Service {
	return &Service{users: users, tokens: tokens, lifecycle: lifecycle}
}

// Authenticate verifies a session token and returns its account. A token
// issued before the account's last password change is rejected.
func (s *Service) Authenticate(ctx context.Context, token string) (*model.Principal, error) {
	claims, err := s.tokens.parse(token)
	if err != nil {
		return nil, err
	}
	user, err := s.users.ByID(ctx, claims.userID)
	if errors.Is(err, ErrUserNotFound) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", middleware.ErrUnavailable, err)
	}
	// Tokens without a stamp predate it and expire within tokenTTL of rollout.
	if claims.passwordStamp != "" && !hmac.Equal([]byte(claims.passwordStamp), []byte(s.tokens.passwordStamp(user.PasswordHash))) {
		return nil, errors.New("token predates a password change")
	}
	return &model.Principal{UserID: user.ID, TenantID: user.TenantID, Role: user.Role}, nil
}

// RegisterResult reports the outcome of a registration. Token is empty when
// ConfirmationRequired is true — the account must confirm its email before it
// can sign in.
type RegisterResult struct {
	Token                string
	ConfirmationRequired bool
}

// Register creates a new account. Each account gets its own tenant and admin
// role. With no lifecycle configured it returns a signed token. When a lifecycle
// requires confirmation it runs the AfterRegister hook and withholds the token.
func (s *Service) Register(ctx context.Context, email, password string) (RegisterResult, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return RegisterResult{}, err
	}

	user := model.User{
		ID:           uuid.NewString(),
		Email:        NormalizeEmail(email),
		PasswordHash: hash,
		TenantID:     uuid.NewString(),
		Role:         "admin",
	}
	if err := s.users.Create(ctx, user); err != nil {
		return RegisterResult{}, err
	}

	if s.lifecycle != nil {
		outcome, err := s.lifecycle.AfterRegister(ctx, account(user))
		if err != nil {
			return RegisterResult{}, err
		}
		if outcome == extension.RequireConfirmation {
			return RegisterResult{ConfirmationRequired: true}, nil
		}
	}

	token, err := s.tokens.Issue(user)
	if err != nil {
		return RegisterResult{}, err
	}
	return RegisterResult{Token: token}, nil
}

// Login verifies credentials and returns a signed token. When a lifecycle is
// configured it also enforces email confirmation, returning
// extension.ErrEmailUnverified for an unconfirmed account.
func (s *Service) Login(ctx context.Context, email, password string) (string, error) {
	user, err := s.users.ByEmail(ctx, NormalizeEmail(email))
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			checkPassword(dummyHash, password)
			return "", ErrInvalidCredentials
		}
		return "", err
	}
	if !checkPassword(user.PasswordHash, password) {
		return "", ErrInvalidCredentials
	}

	if s.lifecycle != nil {
		if err := s.lifecycle.EnsureCanLogin(ctx, account(*user)); err != nil {
			return "", err
		}
	}

	return s.tokens.Issue(*user)
}

// Issue signs a session token for an existing account without checking a
// password. It implements extension.Sessions for embedding applications that
// authenticate users themselves, and applies the same lifecycle gate as Login.
func (s *Service) Issue(ctx context.Context, userID string) (string, error) {
	user, err := s.users.ByID(ctx, userID)
	if err != nil {
		return "", err
	}
	if s.lifecycle != nil {
		if err := s.lifecycle.EnsureCanLogin(ctx, account(*user)); err != nil {
			return "", err
		}
	}
	return s.tokens.Issue(*user)
}

// ChangePassword updates a signed-in user's password after verifying their
// current one. Unlike the CLI reset-password tool (for operators without
// access to the old password) and the hosted email-reset flow, this is the
// self-service path available to every deployment, since it needs no email.
// Every existing session ends; the returned token is the caller's new one.
func (s *Service) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) (string, error) {
	user, err := s.users.ByID(ctx, userID)
	if err != nil {
		return "", err
	}
	if user.PasswordHash == "" {
		return "", ErrNoPassword
	}
	if !checkPassword(user.PasswordHash, currentPassword) {
		return "", ErrInvalidCurrentPassword
	}
	if err := ValidateCredentials(user.Email, newPassword); err != nil {
		return "", err
	}
	if user.PasswordHash, err = HashPassword(newPassword); err != nil {
		return "", err
	}
	if err := s.users.UpdatePasswordHash(ctx, user.ID, user.PasswordHash); err != nil {
		return "", err
	}
	return s.tokens.Issue(*user)
}

var _ extension.Sessions = (*Service)(nil)

func account(u model.User) extension.Account {
	return extension.Account{ID: u.ID, Email: u.Email, TenantID: u.TenantID, Role: u.Role}
}

// NormalizeEmail returns the canonical form emails are stored and looked up in.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
