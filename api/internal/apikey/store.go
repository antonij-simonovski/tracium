package apikey

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tracium/api/internal/model"
)

// ErrNotFound is returned when no active key matches a lookup. It is returned
// both when a key never existed and when it exists but is revoked or belongs to
// another workspace — callers must not be able to distinguish those cases.
var ErrNotFound = errors.New("api key not found")

// Store persists API keys. Handlers depend on this interface rather than the
// concrete type.
type Store interface {
	// Insert records a key by the SHA-256 hash of its token and returns the
	// database-assigned creation time.
	Insert(ctx context.Context, key model.APIKey, hash string) (time.Time, error)
	// List returns every key in a workspace, newest first, including revoked ones
	// so the UI can show history. Secrets and hashes are never returned.
	List(ctx context.Context, workspaceID string) ([]model.APIKey, error)
	// Revoke marks the key revoked. It returns ErrNotFound when no active key with
	// that id exists in workspaceID, which both scopes revocation to the caller's
	// workspace and makes an already-revoked or foreign key indistinguishable.
	Revoke(ctx context.Context, id, workspaceID string) error
	// FindActiveByHash resolves a presented token's hash to its key (carrying the
	// workspace it grants), ignoring revoked keys, and stamps last_used_at.
	// Returns ErrNotFound when there is no active match.
	FindActiveByHash(ctx context.Context, hash string) (*model.APIKey, error)
}

// PostgresStore is the Postgres-backed Store.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) Insert(ctx context.Context, key model.APIKey, hash string) (time.Time, error) {
	var createdAt time.Time
	err := s.pool.QueryRow(ctx,
		`INSERT INTO api_keys (id, workspace_id, created_by, name, prefix, key_hash)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING created_at`,
		key.ID, key.WorkspaceID, key.CreatedBy, key.Name, key.Prefix, hash,
	).Scan(&createdAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("postgres: insert api key: %w", err)
	}
	return createdAt, nil
}

func (s *PostgresStore) List(ctx context.Context, workspaceID string) ([]model.APIKey, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, workspace_id, created_by, name, prefix, created_at, last_used_at, revoked_at
		   FROM api_keys WHERE workspace_id = $1 ORDER BY created_at DESC`, workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list api keys: %w", err)
	}
	defer rows.Close()

	var keys []model.APIKey
	for rows.Next() {
		var k model.APIKey
		if err := rows.Scan(&k.ID, &k.WorkspaceID, &k.CreatedBy, &k.Name, &k.Prefix, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan api key: %w", err)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: iterate api keys: %w", err)
	}
	return keys, nil
}

func (s *PostgresStore) Revoke(ctx context.Context, id, workspaceID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE api_keys SET revoked_at = NOW()
		   WHERE id::text = $1 AND workspace_id = $2 AND revoked_at IS NULL`, id, workspaceID,
	)
	if err != nil {
		return fmt.Errorf("postgres: revoke api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) FindActiveByHash(ctx context.Context, hash string) (*model.APIKey, error) {
	// last_used_at is written at most once a minute to keep the ingest hot path
	// read-only; both CTE arms share a snapshot, so the pre-bump value is returned.
	var k model.APIKey
	err := s.pool.QueryRow(ctx,
		`WITH found AS (
		   SELECT id, workspace_id, created_by, name, prefix, created_at, last_used_at, revoked_at
		     FROM api_keys WHERE key_hash = $1 AND revoked_at IS NULL
		 ), bumped AS (
		   UPDATE api_keys SET last_used_at = NOW()
		     WHERE key_hash = $1 AND revoked_at IS NULL
		       AND (last_used_at IS NULL OR last_used_at < NOW() - INTERVAL '1 minute')
		 )
		 SELECT id, workspace_id, created_by, name, prefix, created_at, last_used_at, revoked_at FROM found`, hash,
	).Scan(&k.ID, &k.WorkspaceID, &k.CreatedBy, &k.Name, &k.Prefix, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: find api key: %w", err)
	}
	return &k, nil
}
