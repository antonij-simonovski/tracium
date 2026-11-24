package mocks

import (
	"context"
	"slices"
	"time"

	"github.com/tracium/api/internal/release"
)

// MockReleaseStore is an in-memory release.Store for unit tests.
type MockReleaseStore struct {
	Reports []release.Report
	Err     error
}

func (m *MockReleaseStore) find(workspaceIDs []string, workflow, version string) *release.Report {
	for i, r := range m.Reports {
		if slices.Contains(workspaceIDs, r.WorkspaceID) && r.Workflow == workflow && r.Version == version {
			return &m.Reports[i]
		}
	}
	return nil
}

func (m *MockReleaseStore) Ensure(_ context.Context, r release.Report) error {
	if m.find([]string{r.WorkspaceID}, r.Workflow, r.Version) == nil {
		r.CreatedAt = time.Now()
		m.Reports = append(m.Reports, r)
	}
	return m.Err
}

func (m *MockReleaseStore) Active(_ context.Context, workspaceID string, since time.Time) ([]release.Report, error) {
	var out []release.Report
	for _, r := range m.Reports {
		if r.WorkspaceID == workspaceID && (r.Status == release.StatusPending || (r.Verdict == "ok" && !r.Rechecked) ||
			(r.Verdict == "regressed" && !r.FinalizedAt.Before(since))) {
			out = append(out, r)
		}
	}
	return out, m.Err
}

func (m *MockReleaseStore) Save(_ context.Context, r release.Report) error {
	if cur := m.find([]string{r.WorkspaceID}, r.Workflow, r.Version); cur != nil && cur.Verdict != "regressed" {
		*cur = r
	}
	return m.Err
}

func (m *MockReleaseStore) List(_ context.Context, workspaceIDs []string, status string, limit int) ([]release.Report, error) {
	out := []release.Report{}
	for _, r := range m.Reports {
		if slices.Contains(workspaceIDs, r.WorkspaceID) && (status == "" || r.Status == status) && len(out) < limit {
			out = append(out, r)
		}
	}
	return out, m.Err
}

func (m *MockReleaseStore) Get(_ context.Context, workspaceIDs []string, workflow, version string) (*release.Report, error) {
	if r := m.find(workspaceIDs, workflow, version); r != nil {
		c := *r
		return &c, m.Err
	}
	return nil, release.ErrNotFound
}

func (m *MockReleaseStore) Verdicts(_ context.Context, workspaceIDs []string, workflow string) (map[string]string, error) {
	out := map[string]string{}
	for _, r := range m.Reports {
		if slices.Contains(workspaceIDs, r.WorkspaceID) && r.Workflow == workflow {
			out[r.Version] = r.Verdict
			if r.Status == release.StatusPending {
				out[r.Version] = "pending"
			}
		}
	}
	return out, m.Err
}
