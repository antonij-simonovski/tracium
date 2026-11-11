package incident

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// evaluatorLockKey is the advisory lock that keeps one evaluator running across replicas.
const evaluatorLockKey = 734162190285

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// TryLock takes the evaluator lock on a dedicated connection. ok is false when
// another session holds it.
func (s *PostgresStore) TryLock(ctx context.Context) (release func(), ok bool, err error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("postgres: acquire lock connection: %w", err)
	}
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, evaluatorLockKey).Scan(&ok); err != nil || !ok {
		conn.Release()
		if err != nil {
			return nil, false, fmt.Errorf("postgres: try evaluator lock: %w", err)
		}
		return nil, false, nil
	}
	return func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, evaluatorLockKey)
		conn.Release()
	}, true, nil
}

const incidentColumns = `id, workspace_id, scope, subject, status, severity, title, version_change, opened_at, updated_at, last_signal_at, resolved_at, resolved_by`

func scanIncident(row pgx.Row) (Incident, error) {
	var i Incident
	err := row.Scan(&i.ID, &i.WorkspaceID, &i.Scope, &i.Subject, &i.Status, &i.Severity, &i.Title, &i.VersionChange,
		&i.OpenedAt, &i.UpdatedAt, &i.LastSignalAt, &i.ResolvedAt, &i.ResolvedBy)
	i.Signals = []Signal{}
	return i, err
}

func (s *PostgresStore) incidents(ctx context.Context, sql string, args ...any) ([]Incident, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: list incidents: %w", err)
	}
	defer rows.Close()
	out := []Incident{}
	for rows.Next() {
		i, err := scanIncident(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan incident: %w", err)
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.loadSignals(ctx, out)
}

func (s *PostgresStore) loadSignals(ctx context.Context, incs []Incident) error {
	if len(incs) == 0 {
		return nil
	}
	ids := make([]string, len(incs))
	at := make(map[string]int, len(incs))
	for i, inc := range incs {
		ids[i], at[inc.ID] = inc.ID, i
	}
	rows, err := s.pool.Query(ctx, `SELECT incident_id, metric, bucket_ms, observed, expected, score, direction, severity, summary, trace_ids
		FROM incident_signals WHERE incident_id = ANY($1) ORDER BY bucket_ms, metric`, ids)
	if err != nil {
		return fmt.Errorf("postgres: list incident signals: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var sig Signal
		if err := rows.Scan(&id, &sig.Metric, &sig.BucketMs, &sig.Observed, &sig.Expected, &sig.Score, &sig.Direction, &sig.Severity, &sig.Summary, &sig.TraceIDs); err != nil {
			return fmt.Errorf("postgres: scan incident signal: %w", err)
		}
		incs[at[id]].Signals = append(incs[at[id]].Signals, sig)
	}
	return rows.Err()
}

func upsertSignals(ctx context.Context, tx pgx.Tx, id string, signals []Signal) error {
	for _, sig := range signals {
		if _, err := tx.Exec(ctx, `INSERT INTO incident_signals (incident_id, metric, bucket_ms, observed, expected, score, direction, severity, summary, trace_ids)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, coalesce($10::text[], '{}'))
			ON CONFLICT (incident_id, metric, bucket_ms) DO UPDATE SET
			  observed = EXCLUDED.observed, expected = EXCLUDED.expected, score = EXCLUDED.score, direction = EXCLUDED.direction,
			  severity = EXCLUDED.severity, summary = EXCLUDED.summary,
			  trace_ids = CASE WHEN cardinality(EXCLUDED.trace_ids) > 0 THEN EXCLUDED.trace_ids ELSE incident_signals.trace_ids END`,
			id, sig.Metric, sig.BucketMs, sig.Observed, sig.Expected, sig.Score, sig.Direction, sig.Severity, sig.Summary, sig.TraceIDs); err != nil {
			return fmt.Errorf("postgres: upsert incident signal: %w", err)
		}
	}
	return nil
}

func (s *PostgresStore) ListOpen(ctx context.Context, workspaceID string) ([]Incident, error) {
	return s.incidents(ctx, `SELECT `+incidentColumns+` FROM incidents WHERE workspace_id = $1 AND status = 'open'`, workspaceID)
}

func (s *PostgresStore) RecentlyResolved(ctx context.Context, workspaceID, scope, subject string, since time.Time) (*Incident, error) {
	incs, err := s.incidents(ctx, `SELECT `+incidentColumns+` FROM incidents
		WHERE workspace_id = $1 AND scope = $2 AND subject = $3 AND status = 'resolved' AND resolved_at >= $4
		ORDER BY resolved_at DESC LIMIT 1`, workspaceID, scope, subject, since)
	if err != nil || len(incs) == 0 {
		return nil, err
	}
	return &incs[0], nil
}

func (s *PostgresStore) Create(ctx context.Context, inc *Incident) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("postgres: begin incident: %w", err)
	}
	defer tx.Rollback(context.Background())
	id := uuid.NewString()
	err = tx.QueryRow(ctx, `INSERT INTO incidents (`+incidentColumns+`)
		VALUES ($1, $2, $3, $4, 'open', $5, $6, $7, $8, $9, $10, NULL, '')
		ON CONFLICT DO NOTHING RETURNING id`,
		id, inc.WorkspaceID, inc.Scope, inc.Subject, inc.Severity, inc.Title, inc.VersionChange, inc.OpenedAt, inc.UpdatedAt, inc.LastSignalAt).Scan(&id)
	created := err == nil
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT id FROM incidents WHERE workspace_id = $1 AND scope = $2 AND subject = $3 AND status = 'open'`,
			inc.WorkspaceID, inc.Scope, inc.Subject).Scan(&id)
	}
	if err != nil {
		return false, fmt.Errorf("postgres: create incident: %w", err)
	}
	if err := upsertSignals(ctx, tx, id, inc.Signals); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("postgres: commit incident: %w", err)
	}
	inc.ID = id
	return created, nil
}

func (s *PostgresStore) Update(ctx context.Context, inc Incident, signals []Signal) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin incident: %w", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `UPDATE incidents SET status = $2, severity = $3, title = $4, version_change = $5,
		updated_at = $6, last_signal_at = $7, resolved_at = $8, resolved_by = $9 WHERE id = $1`,
		inc.ID, inc.Status, inc.Severity, inc.Title, inc.VersionChange, inc.UpdatedAt, inc.LastSignalAt, inc.ResolvedAt, inc.ResolvedBy); err != nil {
		return fmt.Errorf("postgres: update incident: %w", err)
	}
	if err := upsertSignals(ctx, tx, inc.ID, signals); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit incident: %w", err)
	}
	return nil
}

func (s *PostgresStore) Resolve(ctx context.Context, id string, at time.Time, by string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE incidents SET status = 'resolved', resolved_at = $2, updated_at = $2, resolved_by = $3
		WHERE id = $1 AND status = 'open'`, id, at, by)
	if err != nil {
		return fmt.Errorf("postgres: resolve incident: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) List(ctx context.Context, workspaceIDs []string, status Status, page, pageSize int) ([]Incident, int, error) {
	where := `workspace_id = ANY($1) AND ($2 = '' OR status = $2)`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM incidents WHERE `+where, workspaceIDs, string(status)).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("postgres: count incidents: %w", err)
	}
	incs, err := s.incidents(ctx, `SELECT `+incidentColumns+` FROM incidents WHERE `+where+`
		ORDER BY status = 'open' DESC, opened_at DESC, id LIMIT $3 OFFSET $4`, workspaceIDs, string(status), pageSize, (page-1)*pageSize)
	return incs, total, err
}

func (s *PostgresStore) Get(ctx context.Context, workspaceIDs []string, id string) (*Incident, error) {
	incs, err := s.incidents(ctx, `SELECT `+incidentColumns+` FROM incidents WHERE id = $1 AND workspace_id = ANY($2)`, id, workspaceIDs)
	if err != nil {
		return nil, err
	}
	if len(incs) == 0 {
		return nil, ErrNotFound
	}
	return &incs[0], nil
}

func (s *PostgresStore) Mutes(ctx context.Context, workspaceID string) ([]Mute, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, workspace_id, scope, subject, metric, until, created_by, created_at FROM incident_mutes
		WHERE workspace_id = $1 AND (until IS NULL OR until > NOW()) ORDER BY created_at DESC, id`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list mutes: %w", err)
	}
	defer rows.Close()
	out := []Mute{}
	for rows.Next() {
		var m Mute
		if err := rows.Scan(&m.ID, &m.WorkspaceID, &m.Scope, &m.Subject, &m.Metric, &m.Until, &m.CreatedBy, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan mute: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *PostgresStore) AddMute(ctx context.Context, m *Mute) error {
	m.ID = uuid.NewString()
	if err := s.pool.QueryRow(ctx, `INSERT INTO incident_mutes (id, workspace_id, scope, subject, metric, until, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING created_at`,
		m.ID, m.WorkspaceID, m.Scope, m.Subject, m.Metric, m.Until, m.CreatedBy).Scan(&m.CreatedAt); err != nil {
		return fmt.Errorf("postgres: add mute: %w", err)
	}
	return nil
}

func (s *PostgresStore) DeleteMute(ctx context.Context, workspaceID, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM incident_mutes WHERE id = $1 AND workspace_id = $2`, id, workspaceID)
	if err != nil {
		return fmt.Errorf("postgres: delete mute: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) MarkEvaluated(ctx context.Context, workspaceID string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO incident_evaluations (workspace_id, evaluated_at) VALUES ($1, $2)
		ON CONFLICT (workspace_id) DO UPDATE SET evaluated_at = EXCLUDED.evaluated_at`, workspaceID, at)
	if err != nil {
		return fmt.Errorf("postgres: mark evaluated: %w", err)
	}
	return nil
}

func (s *PostgresStore) LastEvaluated(ctx context.Context, workspaceIDs []string) (*time.Time, error) {
	var at *time.Time
	if err := s.pool.QueryRow(ctx, `SELECT min(evaluated_at) FROM incident_evaluations WHERE workspace_id = ANY($1)`, workspaceIDs).Scan(&at); err != nil {
		return nil, fmt.Errorf("postgres: last evaluated: %w", err)
	}
	return at, nil
}
