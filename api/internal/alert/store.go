package alert

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

const channelColumns = `id, workspace_id, kind, name, target, secret, min_severity, notify_resolved, notify_healthy_releases, enabled, created_by, created_at`

func scanChannel(row pgx.Row, extra ...any) (Channel, error) {
	var c Channel
	err := row.Scan(append([]any{&c.ID, &c.WorkspaceID, &c.Kind, &c.Name, &c.Target, &c.Secret, &c.MinSeverity, &c.NotifyResolved, &c.NotifyHealthyReleases, &c.Enabled, &c.CreatedBy, &c.CreatedAt}, extra...)...)
	return c, err
}

func (s *PostgresStore) Channels(ctx context.Context, workspaceID string) ([]Channel, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+channelColumns+` FROM alert_channels WHERE workspace_id = $1 ORDER BY created_at, id`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list alert channels: %w", err)
	}
	defer rows.Close()
	out := []Channel{}
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan alert channel: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *PostgresStore) Channel(ctx context.Context, workspaceID, id string) (*Channel, error) {
	c, err := scanChannel(s.pool.QueryRow(ctx, `SELECT `+channelColumns+` FROM alert_channels WHERE id = $1 AND workspace_id = $2`, id, workspaceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get alert channel: %w", err)
	}
	return &c, nil
}

func (s *PostgresStore) CreateChannel(ctx context.Context, c *Channel) error {
	c.ID = uuid.NewString()
	if err := s.pool.QueryRow(ctx, `INSERT INTO alert_channels (id, workspace_id, kind, name, target, secret, min_severity, notify_resolved, notify_healthy_releases, enabled, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING created_at`,
		c.ID, c.WorkspaceID, c.Kind, c.Name, c.Target, c.Secret, c.MinSeverity, c.NotifyResolved, c.NotifyHealthyReleases, c.Enabled, c.CreatedBy).Scan(&c.CreatedAt); err != nil {
		return fmt.Errorf("postgres: create alert channel: %w", err)
	}
	return nil
}

func (s *PostgresStore) UpdateChannel(ctx context.Context, c Channel) error {
	tag, err := s.pool.Exec(ctx, `UPDATE alert_channels SET name = $3, min_severity = $4, notify_resolved = $5, enabled = $6, notify_healthy_releases = $7
		WHERE id = $1 AND workspace_id = $2`, c.ID, c.WorkspaceID, c.Name, c.MinSeverity, c.NotifyResolved, c.Enabled, c.NotifyHealthyReleases)
	if err != nil {
		return fmt.Errorf("postgres: update alert channel: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) DeleteChannel(ctx context.Context, workspaceID, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM alert_channels WHERE id = $1 AND workspace_id = $2`, id, workspaceID)
	if err != nil {
		return fmt.Errorf("postgres: delete alert channel: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) WorkspaceName(ctx context.Context, workspaceID string) (string, error) {
	var name string
	if err := s.pool.QueryRow(ctx, `SELECT name FROM workspaces WHERE id = $1`, workspaceID).Scan(&name); err != nil {
		return "", fmt.Errorf("postgres: workspace name: %w", err)
	}
	return name, nil
}

func (s *PostgresStore) Enqueue(ctx context.Context, ds []Delivery) error {
	for _, d := range ds {
		if _, err := s.pool.Exec(ctx, `INSERT INTO alert_deliveries (incident_id, channel_id, event, seq, event_at, status, payload, next_attempt_at)
			SELECT $1, $2, $3, (SELECT count(*) FROM alert_deliveries WHERE incident_id = $1 AND channel_id = $2 AND event = $3), $4, 'pending', $5, $6
			ON CONFLICT DO NOTHING`, d.IncidentID, d.ChannelID, d.Event, d.EventAt, d.Payload, d.NextAttemptAt); err != nil {
			return fmt.Errorf("postgres: enqueue alert delivery: %w", err)
		}
	}
	return nil
}

const deliveryColumns = `d.incident_id, d.channel_id, d.event, d.seq, d.event_at, d.status, d.attempts, d.last_error, d.next_attempt_at, d.sent_at`

func deliveryFields(d *Delivery) []any {
	return []any{&d.IncidentID, &d.ChannelID, &d.Event, &d.Seq, &d.EventAt, &d.Status, &d.Attempts, &d.LastError, &d.NextAttemptAt, &d.SentAt}
}

func (s *PostgresStore) Claim(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]Delivery, error) {
	rows, err := s.pool.Query(ctx, `UPDATE alert_deliveries d SET next_attempt_at = $2
		FROM alert_channels c
		WHERE c.id = d.channel_id AND (d.incident_id, d.channel_id, d.event, d.seq) IN (
		  SELECT incident_id, channel_id, event, seq FROM alert_deliveries
		  WHERE status = 'pending' AND next_attempt_at <= $1 ORDER BY next_attempt_at LIMIT $3 FOR UPDATE SKIP LOCKED)
		RETURNING c.id, c.workspace_id, c.kind, c.name, c.target, c.secret, c.min_severity, c.notify_resolved, c.notify_healthy_releases, c.enabled, c.created_by, c.created_at, `+deliveryColumns+`, d.payload`,
		now, now.Add(lease), limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: claim alert deliveries: %w", err)
	}
	defer rows.Close()
	var out []Delivery
	for rows.Next() {
		var d Delivery
		c, err := scanChannel(rows, append(deliveryFields(&d), &d.Payload)...)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan alert delivery: %w", err)
		}
		d.Channel, d.ChannelName, d.ChannelKind = c, c.Name, c.Kind
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *PostgresStore) Finish(ctx context.Context, d Delivery) error {
	if _, err := s.pool.Exec(ctx, `UPDATE alert_deliveries SET status = $5, attempts = $6, last_error = $7, next_attempt_at = $8, sent_at = $9
		WHERE incident_id = $1 AND channel_id = $2 AND event = $3 AND seq = $4`,
		d.IncidentID, d.ChannelID, d.Event, d.Seq, d.Status, d.Attempts, d.LastError, d.NextAttemptAt, d.SentAt); err != nil {
		return fmt.Errorf("postgres: finish alert delivery: %w", err)
	}
	return nil
}

func (s *PostgresStore) Deliveries(ctx context.Context, incidentID string) ([]Delivery, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+deliveryColumns+`, c.name, c.kind FROM alert_deliveries d JOIN alert_channels c ON c.id = d.channel_id
		WHERE d.incident_id = $1 ORDER BY d.event_at, c.name, d.channel_id`, incidentID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list alert deliveries: %w", err)
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		var d Delivery
		if err := rows.Scan(append(deliveryFields(&d), &d.ChannelName, &d.ChannelKind)...); err != nil {
			return nil, fmt.Errorf("postgres: scan alert delivery: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
