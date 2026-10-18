package apikey

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tracium/api/internal/model"
)

// memStore is an in-memory Store for exercising the service without Postgres.
type memStore struct {
	byHash map[string]model.APIKey
	hashOf map[string]string // key id -> hash
}

func newMemStore() *memStore {
	return &memStore{byHash: map[string]model.APIKey{}, hashOf: map[string]string{}}
}

func (m *memStore) Insert(_ context.Context, key model.APIKey, hash string) (time.Time, error) {
	if key.CreatedAt.IsZero() {
		key.CreatedAt = time.Now().UTC()
	}
	m.byHash[hash] = key
	m.hashOf[key.ID] = hash
	return key.CreatedAt, nil
}

func (m *memStore) List(_ context.Context, workspaceID string) ([]model.APIKey, error) {
	var out []model.APIKey
	for _, k := range m.byHash {
		if k.WorkspaceID == workspaceID {
			out = append(out, k)
		}
	}
	return out, nil
}

func (m *memStore) Revoke(_ context.Context, id, workspaceID string) error {
	hash, ok := m.hashOf[id]
	if !ok {
		return ErrNotFound
	}
	k := m.byHash[hash]
	if k.WorkspaceID != workspaceID || k.RevokedAt != nil {
		return ErrNotFound
	}
	now := k.CreatedAt
	k.RevokedAt = &now
	m.byHash[hash] = k
	return nil
}

func (m *memStore) FindActiveByHash(_ context.Context, hash string) (*model.APIKey, error) {
	k, ok := m.byHash[hash]
	if !ok || k.RevokedAt != nil {
		return nil, ErrNotFound
	}
	return &k, nil
}

func TestCreateVerifyRoundTrip(t *testing.T) {
	svc := NewService(newMemStore())
	ctx := context.Background()

	created, err := svc.Create(ctx, "ws-1", "user-1", "ci")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(created.Token, tokenPrefix) {
		t.Fatalf("token missing prefix: %q", created.Token)
	}
	if created.Key.Prefix == created.Token {
		t.Fatal("stored prefix must not be the full token")
	}
	if !strings.HasPrefix(created.Token, created.Key.Prefix) {
		t.Fatalf("display prefix %q is not a prefix of token", created.Key.Prefix)
	}
	if created.Key.WorkspaceID != "ws-1" || created.Key.CreatedBy != "user-1" {
		t.Fatalf("key metadata = %+v, want ws-1 / user-1", created.Key)
	}
	// The create response must carry the store-assigned creation time, not a zero
	// value — the UI shows it immediately after creation.
	if created.Key.CreatedAt.IsZero() {
		t.Fatal("created_at is zero: the store's timestamp was not surfaced")
	}

	// Verify resolves the token to the workspace it grants.
	workspaceID, err := svc.Verify(ctx, created.Token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if workspaceID != "ws-1" {
		t.Fatalf("verify returned %q, want ws-1", workspaceID)
	}
}

func TestVerifyRejectsBadTokens(t *testing.T) {
	svc := NewService(newMemStore())
	ctx := context.Background()
	created, err := svc.Create(ctx, "ws-1", "user-1", "")
	if err != nil {
		t.Fatal(err)
	}

	for name, token := range map[string]string{
		"empty":            "",
		"wrong prefix":     "abc_" + strings.TrimPrefix(created.Token, tokenPrefix),
		"unknown":          tokenPrefix + strings.Repeat("0", len(created.Token)-len(tokenPrefix)),
		"truncated":        created.Token[:len(created.Token)-4],
		"whitespace only":  "   ",
		"token with space": created.Token + " x",
	} {
		if _, err := svc.Verify(ctx, token); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("%s: got %v, want ErrInvalidKey", name, err)
		}
	}
}

func TestRevokeInvalidatesKey(t *testing.T) {
	svc := NewService(newMemStore())
	ctx := context.Background()
	created, err := svc.Create(ctx, "ws-1", "user-1", "temp")
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Revoke(ctx, created.Key.ID, "ws-1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := svc.Verify(ctx, created.Token); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("revoked key still verifies: %v", err)
	}
	// Revoking via a different workspace (or an unknown id) must not succeed.
	if err := svc.Revoke(ctx, created.Key.ID, "ws-2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace revoke: got %v, want ErrNotFound", err)
	}
}

func TestDefaultName(t *testing.T) {
	svc := NewService(newMemStore())
	created, err := svc.Create(context.Background(), "ws-1", "user-1", "   ")
	if err != nil {
		t.Fatal(err)
	}
	if created.Key.Name != "default" {
		t.Fatalf("blank name became %q, want default", created.Key.Name)
	}
}
