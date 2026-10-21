package apikey

import (
	"context"
	"errors"
	tokens "github.com/tracium/api/internal/token"
	"strings"

	"github.com/google/uuid"

	"github.com/tracium/api/internal/model"
)

// ErrInvalidKey is returned by Verify when a presented token is malformed,
// unknown, or revoked. It carries no detail about which, by design.
var ErrInvalidKey = errors.New("invalid api key")

// Service issues, lists, revokes, and verifies API keys. It is the seam the
// handlers and the collector-facing verify endpoint both depend on.
type Service struct {
	store Store
}

// NewService constructs a Service over the given store.
func NewService(store Store) *Service { return &Service{store: store} }

// Created is the result of issuing a key: the metadata plus the one-time
// plaintext token. The token is present only here and is never persisted or
// returned again.
type Created struct {
	Key   model.APIKey `json:"key"`
	Token string       `json:"token"`
}

// Create mints a new key for a workspace and persists its hash. createdBy is
// recorded for provenance only. The returned Token must be surfaced to the
// caller exactly once; it cannot be recovered later.
func (s *Service) Create(ctx context.Context, workspaceID, createdBy, name string) (Created, error) {
	return s.create(ctx, model.APIKey{WorkspaceID: workspaceID, CreatedBy: createdBy, Name: name, Scope: model.ScopeIngest})
}

// CreateRead mints a read key owned by userID. It reads whatever workspaces
// userID can read when it is used, through the query API and MCP only.
func (s *Service) CreateRead(ctx context.Context, userID, name string) (Created, error) {
	return s.create(ctx, model.APIKey{CreatedBy: userID, Name: name, Scope: model.ScopeRead})
}

func (s *Service) create(ctx context.Context, key model.APIKey) (Created, error) {
	key.Name = strings.TrimSpace(key.Name)
	if key.Name == "" {
		key.Name = "default"
	}
	gen, err := generate()
	if err != nil {
		return Created{}, err
	}
	key.ID, key.Prefix = uuid.NewString(), gen.Prefix
	createdAt, err := s.store.Insert(ctx, key, gen.Hash)
	if err != nil {
		return Created{}, err
	}
	// The database assigns created_at; reflect it back so the create response
	// carries the real timestamp rather than a zero value.
	key.CreatedAt = createdAt
	return Created{Key: key, Token: gen.Token}, nil
}

// List returns a workspace's keys (metadata only), newest first.
func (s *Service) List(ctx context.Context, workspaceID string) ([]model.APIKey, error) {
	return s.store.List(ctx, workspaceID)
}

// ListRead returns userID's read keys (metadata only), newest first.
func (s *Service) ListRead(ctx context.Context, userID string) ([]model.APIKey, error) {
	return s.store.ListRead(ctx, userID)
}

// RevokeRead disables one of userID's read keys. Returns ErrNotFound when
// userID has no active read key with that id.
func (s *Service) RevokeRead(ctx context.Context, id, userID string) error {
	return s.store.RevokeRead(ctx, id, userID)
}

// Revoke disables a key in the given workspace. Returns ErrNotFound when no
// active key with that id belongs to workspaceID.
func (s *Service) Revoke(ctx context.Context, id, workspaceID string) error {
	return s.store.Revoke(ctx, id, workspaceID)
}

// Verify resolves a presented plaintext token to the workspace it grants,
// returning ErrInvalidKey for anything malformed, unknown, revoked or read-scoped. This is
// what the ingest path (via the collector's verify call) uses to authenticate a
// sender and decide where its spans land.
func (s *Service) Verify(ctx context.Context, token string) (string, error) {
	key, err := s.Lookup(ctx, token)
	if err != nil {
		return "", err
	}
	if key.Scope != model.ScopeIngest {
		return "", ErrInvalidKey
	}
	return key.WorkspaceID, nil
}

// Lookup is Verify returning the whole key, including who created it.
func (s *Service) Lookup(ctx context.Context, token string) (*model.APIKey, error) {
	token = strings.TrimSpace(token)
	if !tokens.Valid(TokenPrefix, token) {
		return nil, ErrInvalidKey
	}
	key, err := s.store.FindActiveByHash(ctx, tokens.Hash(token))
	if errors.Is(err, ErrNotFound) {
		return nil, ErrInvalidKey
	}
	return key, err
}
