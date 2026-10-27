package oauth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("oauth: not found")

type Client struct {
	ID           string    `json:"client_id"`
	Name         string    `json:"client_name"`
	RedirectURIs []string  `json:"redirect_uris"`
	CreatedAt    time.Time `json:"-"`
}

type Code struct {
	ClientID    string
	UserID      string
	RedirectURI string
	Challenge   string
	Resource    string
}

type Store interface {
	InsertClient(ctx context.Context, c Client) (time.Time, error)
	// Client returns ErrNotFound for an unknown id.
	Client(ctx context.Context, id string) (*Client, error)
	InsertCode(ctx context.Context, hash string, c Code, ttl time.Duration) error
	// ConsumeCode deletes the code and returns it, or ErrNotFound when it is
	// unknown, already used or expired.
	ConsumeCode(ctx context.Context, hash string) (*Code, error)
}

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) InsertClient(ctx context.Context, c Client) (time.Time, error) {
	var createdAt time.Time
	err := s.pool.QueryRow(ctx,
		`INSERT INTO oauth_clients (id, name, redirect_uris) VALUES ($1, $2, $3) RETURNING created_at`,
		c.ID, c.Name, c.RedirectURIs,
	).Scan(&createdAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("postgres: insert oauth client: %w", err)
	}
	return createdAt, nil
}

func (s *PostgresStore) Client(ctx context.Context, id string) (*Client, error) {
	var c Client
	err := s.pool.QueryRow(ctx,
		`SELECT id::text, name, redirect_uris, created_at FROM oauth_clients WHERE id::text = $1`, id,
	).Scan(&c.ID, &c.Name, &c.RedirectURIs, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: find oauth client: %w", err)
	}
	return &c, nil
}

func (s *PostgresStore) InsertCode(ctx context.Context, hash string, c Code, ttl time.Duration) error {
	// Unexchanged codes are cleared here rather than by a background job.
	if _, err := s.pool.Exec(ctx, `DELETE FROM oauth_codes WHERE expires_at < NOW()`); err != nil {
		return fmt.Errorf("postgres: expire oauth codes: %w", err)
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO oauth_codes (code_hash, client_id, user_id, redirect_uri, code_challenge, resource, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW() + make_interval(secs => $7))`,
		hash, c.ClientID, c.UserID, c.RedirectURI, c.Challenge, c.Resource, ttl.Seconds(),
	)
	if err != nil {
		return fmt.Errorf("postgres: insert oauth code: %w", err)
	}
	return nil
}

func (s *PostgresStore) ConsumeCode(ctx context.Context, hash string) (*Code, error) {
	var c Code
	var live bool
	err := s.pool.QueryRow(ctx,
		`DELETE FROM oauth_codes WHERE code_hash = $1
		 RETURNING client_id::text, user_id, redirect_uri, code_challenge, resource, expires_at > NOW()`, hash,
	).Scan(&c.ClientID, &c.UserID, &c.RedirectURI, &c.Challenge, &c.Resource, &live)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !live) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: consume oauth code: %w", err)
	}
	return &c, nil
}
