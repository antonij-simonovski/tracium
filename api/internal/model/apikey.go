package model

import "time"

// APIKey is a bearer credential of one of two scopes.
//
// An ingest key is scoped to one workspace: verifying it both authenticates the
// sender and resolves the workspace its spans belong to. Its CreatedBy is
// provenance only, so it keeps working regardless of that account's membership.
//
// A read key has no workspace (WorkspaceID is empty). It acts as CreatedBy on the
// query API and MCP, reading whatever workspaces that account can read.
//
// The secret itself is never held here: it is returned to the caller once, at
// creation, and only its hash is stored (see the apikey package). This struct is
// the metadata safe to list back to a client — Prefix identifies the key in the
// UI without revealing it.
type APIKey struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspace_id,omitempty"`
	CreatedBy   string     `json:"created_by"`
	Name        string     `json:"name"`
	Prefix      string     `json:"prefix"`
	Scope       string     `json:"scope"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
}

const (
	ScopeIngest = "ingest"
	ScopeRead   = "read"
)
