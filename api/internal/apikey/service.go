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
	name = strings.TrimSpace(name)
	if name == "" {
		name = "default"
	}
	gen, err := generate()
	if err != nil {
		return Created{}, err
	}
	key := model.APIKey{
		ID:          uuid.NewString(),
		WorkspaceID: workspaceID,
		CreatedBy:   createdBy,
		Name:        name,
		Prefix:      gen.Prefix,
	}
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

// Revoke disables a key in the given workspace. Returns ErrNotFound when no
// active key with that id belongs to workspaceID.
func (s *Service) Revoke(ctx context.Context, id, workspaceID string) error {
	return s.store.Revoke(ctx, id, workspaceID)
}

// Verify resolves a presented plaintext token to the workspace it grants,
// returning ErrInvalidKey for anything malformed, unknown, or revoked. This is
// what the ingest path (via the collector's verify call) uses to authenticate a
// sender and decide where its spans land.
func (s *Service) Verify(ctx context.Context, token string) (string, error) {
	token = strings.TrimSpace(token)
	if !tokens.Valid(TokenPrefix, token) {
		return "", ErrInvalidKey
	}
	key, err := s.store.FindActiveByHash(ctx, tokens.Hash(token))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", ErrInvalidKey
		}
		return "", err
	}
	return key.WorkspaceID, nil
}
