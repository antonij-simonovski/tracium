package model

// Principal represents the authenticated identity for an incoming request.
// It is set in context by the auth middleware and consumed by handlers.
type Principal struct {
	UserID   string
	TenantID string
	Role     string
	// WorkspaceID is set when a workspace ingest key authenticated the request.
	// The key reads only that workspace, and UserID is the key's creator, whose
	// entitlements apply.
	WorkspaceID string
}
