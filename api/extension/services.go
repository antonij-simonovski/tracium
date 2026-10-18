// Package extension contains the narrow contracts available to application modules.
package extension

import (
	"context"
	"errors"
	"github.com/tracium/api/internal/middleware"
	"github.com/tracium/api/internal/model"
	"net/http"
	"time"
)

type Principal = model.Principal

func PrincipalFromContext(ctx context.Context) (*Principal, bool) {
	return middleware.PrincipalFromContext(ctx)
}

type Message struct {
	// ID is a stable delivery key. Implementations should deduplicate retries.
	ID      string
	To      string
	Subject string
	Text    string
	// HTML is an optional pre-rendered HTML body. When empty, transports may
	// render their own presentation from Text.
	HTML string
}
type Mailer interface {
	Send(context.Context, Message) error
}

var ErrMailDisabled = errors.New("email delivery is not configured")

type DisabledMailer struct{}

func (DisabledMailer) Send(context.Context, Message) error { return ErrMailDisabled }

// Account identifies a newly registered or authenticating account passed to
// AccountLifecycle hooks.
type Account struct {
	ID       string
	Email    string
	TenantID string
	Role     string
}

// RegistrationOutcome tells the auth service how to complete a registration once
// an AccountLifecycle hook has run.
type RegistrationOutcome int

const (
	// GrantSession issues a session token immediately (the standalone default).
	GrantSession RegistrationOutcome = iota
	// RequireConfirmation withholds the session token; the account must confirm
	// its email before it can sign in.
	RequireConfirmation
)

// ErrEmailUnverified is returned by AccountLifecycle.EnsureCanLogin when an
// account has not confirmed its email. Handlers map it to a 403 response.
var ErrEmailUnverified = errors.New("email not verified")

// AccountLifecycle lets an embedding application (such as the hosted service)
// require email confirmation. The standalone application leaves it unset, so
// registration and login run with no verification step.
type AccountLifecycle interface {
	// AfterRegister runs after a new account is persisted. Returning an error
	// fails the registration. Returning RequireConfirmation withholds the session
	// token; implementations typically send a confirmation email here.
	AfterRegister(context.Context, Account) (RegistrationOutcome, error)
	// EnsureCanLogin runs during login after credentials are verified. Returning
	// ErrEmailUnverified blocks an unconfirmed account from signing in.
	EnsureCanLogin(context.Context, Account) error
}

// Invite describes a newly created workspace invite passed to InviteNotifier.
// Token is the plaintext link token; the dashboard accepts it at /invite/<token>.
type Invite struct {
	ID             string
	WorkspaceID    string
	WorkspaceName  string
	Email          string
	InvitedByEmail string
	Token          string
	ExpiresAt      time.Time
}

// InviteNotifier lets an embedding application deliver invites, for example by
// email. The standalone application leaves it unset and the owner shares the
// link by hand. A returned error is logged and does not fail the invite.
type InviteNotifier interface {
	InviteCreated(context.Context, Invite) error
}

// Sessions issues session tokens for accounts an embedding application has
// authenticated by its own means (for example an external identity provider).
// Obtain it from app.Application.Sessions after assembly.
type Sessions interface {
	// Issue returns the same session token a password login returns for the
	// account. The AccountLifecycle's EnsureCanLogin gate applies, so it can
	// return ErrEmailUnverified.
	Issue(ctx context.Context, userID string) (string, error)
}

type Subject struct {
	UserID      string
	WorkspaceID string
}
type Decision struct {
	Allowed bool   `json:"allowed"`
	Limit   *int64 `json:"limit,omitempty"`
}
type Entitlements interface {
	Check(context.Context, Subject, string) (Decision, error)
}
type CoreEntitlements struct{}

// CoreEntitlements is the default provider. It grants the baseline features the
// core itself gates, so a deployment with no provider configured runs with no
// restrictions. Optional features are withheld unless an embedding application
// supplies its own provider via app.Options.
func (CoreEntitlements) Check(_ context.Context, _ Subject, feature string) (Decision, error) {
	switch feature {
	case "telemetry.read", "workspaces.create", "workspaces.invite", "workspaces.members.add":
		return Decision{Allowed: true}, nil
	default:
		return Decision{}, nil
	}
}

type WorkspaceAccess interface {
	AllowedIDs(context.Context, string) ([]string, error)
}
type Services struct {
	Mail         Mailer
	Entitlements Entitlements
	Workspaces   WorkspaceAccess
}

// Gate is a caller-scoped entitlement check for routes that are not tied to a
// single workspace (user-wide actions like creating a workspace, or reads that
// span every workspace a user owns). It consults the entitlements provider for
// the authenticated user and rejects the request when the feature is not
// allowed. A nil provider (no entitlements configured) allows the request.
func (s Services) Gate(feature string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.Entitlements == nil {
				next.ServeHTTP(w, r)
				return
			}
			p, ok := PrincipalFromContext(r.Context())
			if !ok || p.UserID == "" {
				middleware.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user identity could not be resolved")
				return
			}
			decision, err := s.Entitlements.Check(r.Context(), Subject{UserID: p.UserID}, feature)
			if err != nil {
				middleware.WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "could not check entitlements")
				return
			}
			if !decision.Allowed {
				middleware.WriteError(w, http.StatusForbidden, "FEATURE_UNAVAILABLE", "this feature is not available")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireFeature checks workspace membership before consulting entitlements.
// Extension handlers still validate ownership of any additional objects they use.
// A missing workspace or provider failure never silently grants access.
func (s Services) RequireFeature(feature string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := PrincipalFromContext(r.Context())
			if !ok || p.UserID == "" {
				middleware.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user identity could not be resolved")
				return
			}
			id := r.URL.Query().Get("workspace_id")
			if id == "" {
				middleware.WriteError(w, http.StatusBadRequest, "MISSING_FIELDS", "workspace_id is required")
				return
			}
			if s.Workspaces == nil || s.Entitlements == nil {
				middleware.WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "could not check entitlements")
				return
			}
			ids, err := s.Workspaces.AllowedIDs(r.Context(), p.UserID)
			if err != nil {
				middleware.WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "could not check entitlements")
				return
			}
			allowed := false
			for _, candidate := range ids {
				if candidate == id {
					allowed = true
					break
				}
			}
			if !allowed {
				middleware.WriteError(w, http.StatusForbidden, "FORBIDDEN", "you do not have access to that workspace")
				return
			}
			decision, err := s.Entitlements.Check(r.Context(), Subject{UserID: p.UserID, WorkspaceID: id}, feature)
			if err != nil {
				middleware.WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "could not check entitlements")
				return
			}
			if !decision.Allowed {
				middleware.WriteError(w, http.StatusForbidden, "FEATURE_UNAVAILABLE", "this feature is not available")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
