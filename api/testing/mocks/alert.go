package mocks

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tracium/api/internal/alert"
)

// MockAlertStore is an in-memory alert.Store for unit tests.
type MockAlertStore struct {
	mu         sync.Mutex
	ChannelSet []alert.Channel
	Queue      []alert.Delivery
	Names      map[string]string
	RuleSet    []alert.Rule
	Err        error
	nextID     int
}

func (m *MockAlertStore) Channels(_ context.Context, workspaceID string) ([]alert.Channel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []alert.Channel{}
	for _, c := range m.ChannelSet {
		if c.WorkspaceID == workspaceID {
			out = append(out, c)
		}
	}
	return out, m.Err
}

func (m *MockAlertStore) Channel(_ context.Context, workspaceID, id string) (*alert.Channel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.ChannelSet {
		if c.ID == id && c.WorkspaceID == workspaceID {
			return &c, m.Err
		}
	}
	return nil, alert.ErrNotFound
}

func (m *MockAlertStore) CreateChannel(_ context.Context, c *alert.Channel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	c.ID = fmt.Sprintf("ch-%d", m.nextID)
	m.ChannelSet = append(m.ChannelSet, *c)
	return m.Err
}

func (m *MockAlertStore) UpdateChannel(_ context.Context, c alert.Channel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.ChannelSet {
		if m.ChannelSet[i].ID == c.ID && m.ChannelSet[i].WorkspaceID == c.WorkspaceID {
			m.ChannelSet[i] = c
			return m.Err
		}
	}
	return alert.ErrNotFound
}

func (m *MockAlertStore) DeleteChannel(_ context.Context, workspaceID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, c := range m.ChannelSet {
		if c.ID == id && c.WorkspaceID == workspaceID {
			m.ChannelSet = append(m.ChannelSet[:i], m.ChannelSet[i+1:]...)
			return m.Err
		}
	}
	return alert.ErrNotFound
}

func (m *MockAlertStore) WorkspaceName(_ context.Context, workspaceID string) (string, error) {
	return m.Names[workspaceID], m.Err
}

func (m *MockAlertStore) Enqueue(_ context.Context, ds []alert.Delivery) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range ds {
		seq, dup := 0, false
		for _, q := range m.Queue {
			if q.IncidentID == d.IncidentID && q.ChannelID == d.ChannelID && q.Event == d.Event {
				seq++
				dup = dup || q.EventAt.Equal(d.EventAt)
			}
		}
		if !dup {
			d.Seq = seq
			m.Queue = append(m.Queue, d)
		}
	}
	return m.Err
}

func (m *MockAlertStore) Claim(_ context.Context, now time.Time, lease time.Duration, limit int) ([]alert.Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []alert.Delivery
	for i := range m.Queue {
		d := &m.Queue[i]
		if len(out) == limit || d.Status != alert.StatusPending || d.NextAttemptAt.After(now) {
			continue
		}
		d.NextAttemptAt = now.Add(lease)
		claimed := *d
		for _, c := range m.ChannelSet {
			if c.ID == d.ChannelID {
				claimed.Channel = c
			}
		}
		out = append(out, claimed)
	}
	return out, m.Err
}

func (m *MockAlertStore) Finish(_ context.Context, d alert.Delivery) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, q := range m.Queue {
		if q.IncidentID == d.IncidentID && q.ChannelID == d.ChannelID && q.Event == d.Event && q.Seq == d.Seq {
			d.Channel = alert.Channel{}
			m.Queue[i] = d
		}
	}
	return m.Err
}

func (m *MockAlertStore) Deliveries(_ context.Context, incidentID string) ([]alert.Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []alert.Delivery{}
	for _, d := range m.Queue {
		if d.IncidentID == incidentID {
			out = append(out, d)
		}
	}
	return out, m.Err
}

func (m *MockAlertStore) Rules(_ context.Context, workspaceID string) ([]alert.Rule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []alert.Rule{}
	for _, r := range m.RuleSet {
		if r.WorkspaceID == workspaceID {
			out = append(out, r)
		}
	}
	return out, m.Err
}

func (m *MockAlertStore) Rule(_ context.Context, workspaceID, id string) (*alert.Rule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.RuleSet {
		if r.ID == id && r.WorkspaceID == workspaceID {
			return &r, m.Err
		}
	}
	return nil, alert.ErrRuleNotFound
}

func (m *MockAlertStore) CreateRule(_ context.Context, r *alert.Rule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	r.ID = fmt.Sprintf("rule-%d", m.nextID)
	m.RuleSet = append(m.RuleSet, *r)
	return m.Err
}

func (m *MockAlertStore) UpdateRule(_ context.Context, r alert.Rule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.RuleSet {
		if m.RuleSet[i].ID == r.ID && m.RuleSet[i].WorkspaceID == r.WorkspaceID {
			m.RuleSet[i] = r
			return m.Err
		}
	}
	return alert.ErrRuleNotFound
}

func (m *MockAlertStore) DeleteRule(_ context.Context, workspaceID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.RuleSet {
		if r.ID == id && r.WorkspaceID == workspaceID {
			m.RuleSet = append(m.RuleSet[:i], m.RuleSet[i+1:]...)
			return m.Err
		}
	}
	return alert.ErrRuleNotFound
}
