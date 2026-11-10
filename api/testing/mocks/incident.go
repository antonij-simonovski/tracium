package mocks

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/tracium/api/internal/incident"
)

// MockIncidentStore is an in-memory incident.Store for unit tests.
type MockIncidentStore struct {
	Incidents []incident.Incident
	MuteList  []incident.Mute
	Err       error
	nextID    int
}

func (m *MockIncidentStore) find(id string) *incident.Incident {
	for i := range m.Incidents {
		if m.Incidents[i].ID == id {
			return &m.Incidents[i]
		}
	}
	return nil
}

func (m *MockIncidentStore) ListOpen(_ context.Context, workspaceID string) ([]incident.Incident, error) {
	var out []incident.Incident
	for _, inc := range m.Incidents {
		if inc.WorkspaceID == workspaceID && inc.Status == incident.StatusOpen {
			out = append(out, inc)
		}
	}
	return out, m.Err
}

func (m *MockIncidentStore) RecentlyResolved(_ context.Context, workspaceID, scope, subject string, since time.Time) (*incident.Incident, error) {
	var latest *incident.Incident
	for i, inc := range m.Incidents {
		if inc.WorkspaceID == workspaceID && inc.Scope == scope && inc.Subject == subject && inc.Status == incident.StatusResolved &&
			!inc.ResolvedAt.Before(since) && (latest == nil || inc.ResolvedAt.After(*latest.ResolvedAt)) {
			latest = &m.Incidents[i]
		}
	}
	if latest == nil {
		return nil, m.Err
	}
	c := *latest
	return &c, m.Err
}

func (m *MockIncidentStore) Create(_ context.Context, inc *incident.Incident) (bool, error) {
	if m.Err != nil {
		return false, m.Err
	}
	m.nextID++
	inc.ID = fmt.Sprintf("inc-%d", m.nextID)
	m.Incidents = append(m.Incidents, *inc)
	return true, nil
}

func (m *MockIncidentStore) Update(_ context.Context, inc incident.Incident, _ []incident.Signal) error {
	if p := m.find(inc.ID); p != nil {
		*p = inc
	}
	return m.Err
}

func (m *MockIncidentStore) Resolve(_ context.Context, id string, at time.Time, by string) error {
	p := m.find(id)
	if p == nil || p.Status != incident.StatusOpen {
		return incident.ErrNotFound
	}
	p.Status, p.ResolvedAt, p.ResolvedBy = incident.StatusResolved, &at, by
	return m.Err
}

func (m *MockIncidentStore) List(_ context.Context, workspaceIDs []string, status incident.Status, page, pageSize int) ([]incident.Incident, int, error) {
	out := []incident.Incident{}
	for _, inc := range m.Incidents {
		if slices.Contains(workspaceIDs, inc.WorkspaceID) && (status == "" || inc.Status == status) {
			out = append(out, inc)
		}
	}
	return out, len(out), m.Err
}

func (m *MockIncidentStore) Get(_ context.Context, workspaceIDs []string, id string) (*incident.Incident, error) {
	if p := m.find(id); p != nil && slices.Contains(workspaceIDs, p.WorkspaceID) {
		c := *p
		return &c, m.Err
	}
	return nil, incident.ErrNotFound
}

func (m *MockIncidentStore) Mutes(_ context.Context, workspaceID string) ([]incident.Mute, error) {
	var out []incident.Mute
	for _, mu := range m.MuteList {
		if mu.WorkspaceID == workspaceID {
			out = append(out, mu)
		}
	}
	return out, m.Err
}

func (m *MockIncidentStore) AddMute(_ context.Context, mu *incident.Mute) error {
	mu.ID = fmt.Sprintf("mute-%d", len(m.MuteList)+1)
	m.MuteList = append(m.MuteList, *mu)
	return m.Err
}

func (m *MockIncidentStore) DeleteMute(_ context.Context, workspaceID, id string) error {
	for i, mu := range m.MuteList {
		if mu.ID == id && mu.WorkspaceID == workspaceID {
			m.MuteList = slices.Delete(m.MuteList, i, i+1)
			return m.Err
		}
	}
	return incident.ErrNotFound
}
