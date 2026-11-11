// Package alert delivers incident events to a workspace's channels: Slack
// incoming webhooks, signed generic webhooks and email. Notify only enqueues;
// Flush sends, retrying failures with backoff.
package alert

import (
	"context"
	"errors"
	"time"
)

const (
	KindSlack   = "slack"
	KindWebhook = "webhook"
	KindEmail   = "email"

	StatusPending = "pending"
	StatusSent    = "sent"
	StatusFailed  = "failed"
)

var ErrNotFound = errors.New("alert channel not found")

type Channel struct {
	ID             string    `json:"id"`
	WorkspaceID    string    `json:"workspace_id"`
	Kind           string    `json:"kind"`
	Name           string    `json:"name"`
	Target         string    `json:"target"`
	Secret         string    `json:"-"`
	MinSeverity    string    `json:"min_severity"`
	NotifyResolved bool      `json:"notify_resolved"`
	Enabled        bool      `json:"enabled"`
	CreatedBy      string    `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
}

// Delivery is one event queued for one channel. Seq counts earlier deliveries
// of the same event, so a reopened incident is delivered again.
type Delivery struct {
	IncidentID    string     `json:"incident_id"`
	ChannelID     string     `json:"channel_id"`
	ChannelName   string     `json:"channel_name"`
	ChannelKind   string     `json:"channel_kind"`
	Event         string     `json:"event"`
	Seq           int        `json:"seq"`
	EventAt       time.Time  `json:"event_at"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts"`
	LastError     string     `json:"last_error"`
	NextAttemptAt time.Time  `json:"next_attempt_at"`
	SentAt        *time.Time `json:"sent_at"`
	Payload       Message    `json:"-"`
	Channel       Channel    `json:"-"`
}

// Store persists channels and the delivery queue. Channel reads and writes are
// scoped to one workspace.
type Store interface {
	Channels(ctx context.Context, workspaceID string) ([]Channel, error)
	Channel(ctx context.Context, workspaceID, id string) (*Channel, error)
	CreateChannel(ctx context.Context, ch *Channel) error
	UpdateChannel(ctx context.Context, ch Channel) error
	DeleteChannel(ctx context.Context, workspaceID, id string) error
	WorkspaceName(ctx context.Context, workspaceID string) (string, error)
	// Enqueue inserts deliveries, skipping any already queued for the same event_at.
	Enqueue(ctx context.Context, ds []Delivery) error
	// Claim leases up to limit pending deliveries due at now, with their
	// channels, by pushing their next attempt to now+lease. Concurrent claims
	// never return the same delivery.
	Claim(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]Delivery, error)
	// Finish writes a claimed delivery's status, attempts, error and times.
	Finish(ctx context.Context, d Delivery) error
	Deliveries(ctx context.Context, incidentID string) ([]Delivery, error)
}

// Options holds delivery settings. Backoff[i] is the wait after attempt i+1
// fails; after len(Backoff)+1 attempts a delivery is failed.
type Options struct {
	AllowPrivateURLs bool
	PublicURL        string
	EmailEnabled     bool
	Backoff          []time.Duration
	FlushInterval    time.Duration
	Lease            time.Duration
	BatchSize        int
	Timeout          time.Duration
	MaxResponseBytes int64
}

func DefaultOptions() Options {
	return Options{
		Backoff:          []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, time.Hour},
		FlushInterval:    30 * time.Second,
		Lease:            5 * time.Minute,
		BatchSize:        20,
		Timeout:          5 * time.Second,
		MaxResponseBytes: 4 << 10,
	}
}
