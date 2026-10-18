package handler

import (
	"context"
	"fmt"
	"net/http"

	"github.com/tracium/api/extension"
)

// Entitlement features checked by member management.
const (
	// InviteFeature is checked before creating an invite.
	InviteFeature = "workspaces.invite"
	// MemberAddFeature is checked before an account joins a workspace, either by
	// accepting an invite or by being added directly.
	MemberAddFeature = "workspaces.members.add"
)

// entitlementDenial is a refused entitlement check, ready to write as a response.
type entitlementDenial struct {
	status  int
	code    string
	message string
}

func (d *entitlementDenial) Error() string { return d.message }

func (d *entitlementDenial) respond(w http.ResponseWriter) {
	respondError(w, d.status, d.code, d.message)
}

// checkMemberEntitlement consults the provider for a member-management feature.
// A nil provider allows everything. A denial that carries a limit means the
// workspace is full and is reported as MEMBER_LIMIT_REACHED.
func checkMemberEntitlement(ctx context.Context, ents extension.Entitlements, subject extension.Subject, feature string) error {
	if ents == nil {
		return nil
	}
	decision, err := ents.Check(ctx, subject, feature)
	if err != nil {
		return &entitlementDenial{http.StatusServiceUnavailable, "UNAVAILABLE", "could not check entitlements"}
	}
	if decision.Allowed {
		return nil
	}
	if decision.Limit != nil {
		return &entitlementDenial{http.StatusForbidden, "MEMBER_LIMIT_REACHED", fmt.Sprintf("This workspace has reached its limit of %d members.", *decision.Limit)}
	}
	return &entitlementDenial{http.StatusForbidden, "FEATURE_UNAVAILABLE", "adding members is not available for this workspace"}
}
