package workspace

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tracium/api/internal/model"
)

// Roles a member can hold in a workspace.
const (
	RoleOwner  = "owner"
	RoleMember = "member"
)

// Store is the data access interface for workspaces. Handlers depend on this
// interface, never on a concrete database type.
type Store interface {
	// List returns every workspace the user can access (any workspace they are a
	// member of), with the user's own role in each.
	List(ctx context.Context, userID string) ([]model.Workspace, error)
	// Create inserts a workspace and makes its creator (ws.UserID) the owner
	// member in one transaction.
	Create(ctx context.Context, ws model.Workspace) error
	// Delete removes the workspace, allowed only for its owner. Returns
	// ErrNotFound when no workspace with that id is owned by userID.
	Delete(ctx context.Context, id, userID string) error
	// AllowedIDs returns the ids of every workspace the user is a member of. It
	// is the access-control primitive the data handlers scope reads to.
	AllowedIDs(ctx context.Context, userID string) ([]string, error)
	// IsOwner reports whether userID owns the workspace. Used to gate member
	// management.
	IsOwner(ctx context.Context, workspaceID, userID string) (bool, error)
	// GrantMember adds userID as a member (never an owner) at the owner's
	// request. Under the workspace lock it returns early for an existing member
	// (no seat taken), otherwise runs allow, adds the member and closes their
	// open invites. invited tells allow whether an open invite already holds this
	// user's seat; allow's error aborts the add and is returned unchanged.
	GrantMember(ctx context.Context, workspaceID, userID string, allow func(ctx context.Context, invited bool) error) error
	// RemoveMember revokes a user's access. Removing the owner is refused with
	// ErrCannotRemoveOwner.
	RemoveMember(ctx context.Context, workspaceID, userID string) error
	ListMembers(ctx context.Context, workspaceID string) ([]model.WorkspaceMember, error)
}

// ErrNotFound is the sentinel error returned when a workspace does not exist for
// the requesting user. Handlers check for this specifically to return 404 vs 500.
// It is deliberately also returned when the workspace exists but belongs to
// another user — never reveal another user's workspaces.
var ErrNotFound = errors.New("workspace not found")

// ErrCannotRemoveOwner is returned when a request tries to remove the owner from
// a workspace's members. The owner is removed only by deleting the workspace.
var ErrCannotRemoveOwner = errors.New("cannot remove the workspace owner")

// PostgresStore persists workspaces in Postgres, keyed by user ID.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// List returns every workspace the user is a member of, ordered by creation
// time. Role is the user's own role in each workspace; members is that
// workspace's total member count.
func (s *PostgresStore) List(ctx context.Context, userID string) ([]model.Workspace, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT w.id, w.name, w.slug, w.env, wm.role,
		        (SELECT count(*) FROM workspace_members m WHERE m.workspace_id = w.id) AS members
		   FROM workspaces w
		   JOIN workspace_members wm ON wm.workspace_id = w.id AND wm.user_id = $1
		  ORDER BY w.created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("workspace store: list: %w", err)
	}
	defer rows.Close()

	var workspaces []model.Workspace
	for rows.Next() {
		var ws model.Workspace
		if err := rows.Scan(&ws.ID, &ws.Name, &ws.Slug, &ws.Env, &ws.Role, &ws.Members); err != nil {
			return nil, fmt.Errorf("workspace store: scan: %w", err)
		}
		workspaces = append(workspaces, ws)
	}
	return workspaces, rows.Err()
}

// Create inserts a workspace and its owner membership in one transaction, so a
// workspace can never exist without an owner able to see it.
func (s *PostgresStore) Create(ctx context.Context, ws model.Workspace) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("workspace store: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful Commit

	if _, err := tx.Exec(ctx,
		`INSERT INTO workspaces (id, user_id, name, slug, env) VALUES ($1, $2, $3, $4, $5)`,
		ws.ID, ws.UserID, ws.Name, ws.Slug, ws.Env); err != nil {
		return fmt.Errorf("workspace store: create: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, $3)`,
		ws.ID, ws.UserID, RoleOwner); err != nil {
		return fmt.Errorf("workspace store: create owner membership: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("workspace store: commit: %w", err)
	}
	return nil
}

// Delete removes a workspace and all its memberships, allowed only for the
// owner. Returns ErrNotFound when no workspace with that id is owned by userID —
// which also covers "belongs to someone else", so it never reveals another
// account's workspace.
func (s *PostgresStore) Delete(ctx context.Context, id, userID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("workspace store: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful Commit

	tag, err := tx.Exec(ctx,
		`DELETE FROM workspaces WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("workspace store: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM workspace_members WHERE workspace_id = $1`, id); err != nil {
		return fmt.Errorf("workspace store: delete members: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("workspace store: commit: %w", err)
	}
	return nil
}

// AllowedIDs returns the ids of every workspace the user can access.
func (s *PostgresStore) AllowedIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT workspace_id FROM workspace_members WHERE user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("workspace store: allowed ids: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("workspace store: scan id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// IsOwner reports whether userID owns the workspace.
func (s *PostgresStore) IsOwner(ctx context.Context, workspaceID, userID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM workspaces WHERE id = $1 AND user_id = $2)`,
		workspaceID, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("workspace store: is owner: %w", err)
	}
	return exists, nil
}

// GrantMember implements Store.
func (s *PostgresStore) GrantMember(ctx context.Context, workspaceID, userID string, allow func(ctx context.Context, invited bool) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("workspace store: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful Commit
	if err := lockWorkspace(ctx, tx, workspaceID); err != nil {
		return err
	}

	var member, invited bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM workspace_members WHERE workspace_id = $1 AND user_id = $2),
		        EXISTS (SELECT 1 FROM workspace_invites i JOIN users u ON lower(u.email) = i.email
		                 WHERE i.workspace_id = $1 AND u.id::text = $2
		                   AND i.accepted_at IS NULL AND i.revoked_at IS NULL AND i.expires_at > NOW())`,
		workspaceID, userID).Scan(&member, &invited); err != nil {
		return fmt.Errorf("workspace store: check member: %w", err)
	}
	if member {
		return nil
	}
	if allow != nil {
		if err := allow(ctx, invited); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, $3)`,
		workspaceID, userID, RoleMember); err != nil {
		return fmt.Errorf("workspace store: add member: %w", err)
	}
	// The direct add fulfils any open invite, which would otherwise keep holding
	// a second seat for the same person.
	if _, err := tx.Exec(ctx,
		`UPDATE workspace_invites i SET accepted_at = NOW(), accepted_by = $2
		   FROM users u
		  WHERE i.workspace_id = $1 AND u.id::text = $2 AND lower(u.email) = i.email
		    AND i.accepted_at IS NULL AND i.revoked_at IS NULL`,
		workspaceID, userID); err != nil {
		return fmt.Errorf("workspace store: close invites: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("workspace store: commit: %w", err)
	}
	return nil
}

// RemoveMember revokes a user's access, refusing to remove the owner.
func (s *PostgresStore) RemoveMember(ctx context.Context, workspaceID, userID string) error {
	owner, err := s.IsOwner(ctx, workspaceID, userID)
	if err != nil {
		return err
	}
	if owner {
		return ErrCannotRemoveOwner
	}
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM workspace_members WHERE workspace_id = $1 AND user_id = $2`,
		workspaceID, userID)
	if err != nil {
		return fmt.Errorf("workspace store: remove member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
