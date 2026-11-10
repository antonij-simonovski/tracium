// Package incident turns detected anomalies into stored incidents: one per
// subject (a workflow, a client or the workspace) that open, escalate, resolve
// and can be muted. Every state change is emitted to a Notifier.
package incident

import (
	"context"
	"errors"
	"time"

	"github.com/tracium/api/internal/model"
)

type Status string

const (
	StatusOpen     Status = "open"
	StatusResolved Status = "resolved"
)

type EventType string

const (
	EventOpened    EventType = "opened"
	EventEscalated EventType = "escalated"
	EventResolved  EventType = "resolved"
)

var ErrNotFound = errors.New("incident not found")

// AccuracyMetric is a drop in reviewed accuracy between two workflow versions.
const AccuracyMetric = "review_accuracy"

type Incident struct {
	ID            string               `json:"id"`
	WorkspaceID   string               `json:"workspace_id"`
	Scope         string               `json:"scope"`
	Subject       string               `json:"subject"`
	Status        Status               `json:"status"`
	Severity      string               `json:"severity"`
	Title         string               `json:"title"`
	OpenedAt      time.Time            `json:"opened_at"`
	UpdatedAt     time.Time            `json:"updated_at"`
	ResolvedAt    *time.Time           `json:"resolved_at"`
	ResolvedBy    string               `json:"resolved_by"`
	LastSignalAt  time.Time            `json:"last_signal_at"`
	VersionChange *model.VersionChange `json:"version_change"`
	Signals       []Signal             `json:"signals"`
}

type Signal struct {
	Metric    string   `json:"metric"`
	BucketMs  int64    `json:"bucket_ms"`
	Observed  float64  `json:"observed"`
	Expected  float64  `json:"expected"`
	Score     float64  `json:"score"`
	Direction string   `json:"direction"`
	Severity  string   `json:"severity"`
	Summary   string   `json:"summary"`
	TraceIDs  []string `json:"trace_ids"`
}

type Mute struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspace_id"`
	Scope       string     `json:"scope"`
	Subject     string     `json:"subject"`
	Metric      string     `json:"metric"`
	Until       *time.Time `json:"until"`
	CreatedBy   string     `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
}

type Event struct {
	Type     EventType
	Incident Incident
}

type Notifier interface {
	Notify(ctx context.Context, e Event) error
}

type NopNotifier struct{}

func (NopNotifier) Notify(context.Context, Event) error { return nil }

// Store persists incidents. Reads taking workspaceIDs are scoped to them; an
// empty scope matches nothing.
type Store interface {
	ListOpen(ctx context.Context, workspaceID string) ([]Incident, error)
	// RecentlyResolved is the subject's latest incident resolved at or after since, or nil.
	RecentlyResolved(ctx context.Context, workspaceID, scope, subject string, since time.Time) (*Incident, error)
	// Create stores a new open incident and its signals and sets inc.ID. When the
	// subject already has an open incident, the signals join it instead,
	// inc.ID is set to it, and created is false.
	Create(ctx context.Context, inc *Incident) (created bool, err error)
	// Update writes the incident's status, severity, title and times, and upserts signals.
	Update(ctx context.Context, inc Incident, signals []Signal) error
	// Resolve resolves an open incident; ErrNotFound when it is not open.
	Resolve(ctx context.Context, id string, at time.Time, by string) error
	List(ctx context.Context, workspaceIDs []string, status Status, page, pageSize int) ([]Incident, int, error)
	Get(ctx context.Context, workspaceIDs []string, id string) (*Incident, error)
	Mutes(ctx context.Context, workspaceID string) ([]Mute, error)
	AddMute(ctx context.Context, m *Mute) error
	DeleteMute(ctx context.Context, workspaceID, id string) error
}
