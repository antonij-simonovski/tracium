package mocks

import (
	"context"

	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/workspace"
)

// MockWorkspaceStore is an in-memory implementation of workspace.Store for use
// in handler unit tests. It never touches a real database.
type MockWorkspaceStore struct {
	Workspaces []model.Workspace
	Members    map[string][]model.WorkspaceMember // keyed by workspace id

	// Configurable errors — set these to simulate failures.
	ListErr   error
	CreateErr error
	DeleteErr error

	// AllowedIDsErr simulates an access-lookup failure.
	AllowedIDsErr  error
	ListMembersErr error

	// Call counters — inspect these in tests.
	ListCallCount   int
	CreateCallCount int
	DeleteCallCount int
}

// List returns the stored workspaces belonging to the given user.
func (m *MockWorkspaceStore) List(_ context.Context, userID string) ([]model.Workspace, error) {
	m.ListCallCount++
	if m.ListErr != nil {
		return nil, m.ListErr
	}

	var result []model.Workspace
	for _, ws := range m.Workspaces {
		if ws.UserID == userID {
			result = append(result, ws)
		}
	}
	return result, nil
}

// Create appends a workspace to the mock store.
func (m *MockWorkspaceStore) Create(_ context.Context, ws model.Workspace) error {
	m.CreateCallCount++
	if m.CreateErr != nil {
		return m.CreateErr
	}
	m.Workspaces = append(m.Workspaces, ws)
	return nil
}

// Delete removes the workspace owned by userID, mirroring the real store's
// scoping: it returns workspace.ErrNotFound when no workspace matches both id
// and userID.
func (m *MockWorkspaceStore) Delete(_ context.Context, id, userID string) error {
	m.DeleteCallCount++
	if m.DeleteErr != nil {
		return m.DeleteErr
	}

	for i, ws := range m.Workspaces {
		if ws.ID == id && ws.UserID == userID {
			m.Workspaces = append(m.Workspaces[:i], m.Workspaces[i+1:]...)
			return nil
		}
	}
	return workspace.ErrNotFound
}

// AllowedIDs returns the ids of the workspaces the user can access. The mock
// treats ownership as membership (every stored workspace the user owns).
func (m *MockWorkspaceStore) AllowedIDs(_ context.Context, userID string) ([]string, error) {
	if m.AllowedIDsErr != nil {
		return nil, m.AllowedIDsErr
	}
	var ids []string
	for _, ws := range m.Workspaces {
		if ws.UserID == userID {
			ids = append(ids, ws.ID)
		}
	}
	return ids, nil
}

// IsOwner reports whether the user owns the workspace in the mock store.
func (m *MockWorkspaceStore) IsOwner(_ context.Context, workspaceID, userID string) (bool, error) {
	for _, ws := range m.Workspaces {
		if ws.ID == workspaceID && ws.UserID == userID {
			return true, nil
		}
	}
	return false, nil
}

func (m *MockWorkspaceStore) GrantMember(ctx context.Context, _, _ string, allow func(context.Context, bool) error) error {
	if allow != nil {
		return allow(ctx, false)
	}
	return nil
}

// RemoveMember is a no-op in the mock.
func (m *MockWorkspaceStore) RemoveMember(_ context.Context, _, _ string) error { return nil }

func (m *MockWorkspaceStore) ListMembers(_ context.Context, workspaceID string) ([]model.WorkspaceMember, error) {
	if m.ListMembersErr != nil {
		return nil, m.ListMembersErr
	}
	return m.Members[workspaceID], nil
}

// NewTestWorkspace returns a Workspace fixture owned by the given user.
func NewTestWorkspace(id, userID string) model.Workspace {
	return model.Workspace{
		ID:      id,
		UserID:  userID,
		Name:    "test-workspace",
		Slug:    "test-workspace",
		Env:     "production",
		Role:    workspace.RoleOwner,
		Members: 1,
	}
}
