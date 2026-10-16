package model

// Principal represents the authenticated identity for an incoming request.
// It is set in context by the auth middleware and consumed by handlers.
type Principal struct {
	UserID   string
	TenantID string
	Role     string
	// WorkspaceID is set instead of UserID when a workspace ingest key
	// authenticated the request; the key reads only that workspace.
	WorkspaceID string
}
