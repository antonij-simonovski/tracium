package release

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tracium/api/internal/anomaly"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

const reportColumns = `workspace_id, workflow, version, prev_version, first_seen_at, status, verdict, runs, prev_runs, findings, rechecked, created_at, finalized_at`

func (s *PostgresStore) Ensure(ctx context.Context, r Report) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO release_reports (workspace_id, workflow, version, prev_version, first_seen_at, status)
VALUES ($1, $2, $3, $4, $5, 'pending') ON CONFLICT DO NOTHING`, r.WorkspaceID, r.Workflow, r.Version, r.PrevVersion, r.FirstSeenAt)
	if err != nil {
		return fmt.Errorf("postgres: ensure release report: %w", err)
	}
	return nil
}

func (s *PostgresStore) Active(ctx context.Context, workspaceID string, since time.Time) ([]Report, error) {
	return s.reports(ctx, `SELECT `+reportColumns+` FROM release_reports WHERE workspace_id = $1 AND (status = 'pending'
 OR (verdict = 'ok' AND NOT rechecked) OR (verdict = 'regressed' AND finalized_at >= $2))`, workspaceID, since)
}

func (s *PostgresStore) Save(ctx context.Context, r Report) error {
	findings := r.Findings
	if findings == nil {
		findings = []anomaly.Finding{}
	}
	_, err := s.pool.Exec(ctx, `UPDATE release_reports SET status = $4, verdict = $5, runs = $6, prev_runs = $7, findings = $8,
 rechecked = $9, finalized_at = $10 WHERE workspace_id = $1 AND workflow = $2 AND version = $3 AND verdict <> 'regressed'`,
		r.WorkspaceID, r.Workflow, r.Version, r.Status, r.Verdict, r.Runs, r.PrevRuns, findings, r.Rechecked, r.FinalizedAt)
	if err != nil {
		return fmt.Errorf("postgres: save release report: %w", err)
	}
	return nil
}

func (s *PostgresStore) List(ctx context.Context, workspaceIDs []string, status string, limit int) ([]Report, error) {
	return s.reports(ctx, `SELECT `+reportColumns+` FROM release_reports WHERE workspace_id = ANY($1) AND ($2 = '' OR status = $2)
 ORDER BY created_at DESC, workflow, version LIMIT $3`, workspaceIDs, status, limit)
}

func (s *PostgresStore) Get(ctx context.Context, workspaceIDs []string, workflow, version string) (*Report, error) {
	out, err := s.reports(ctx, `SELECT `+reportColumns+` FROM release_reports WHERE workspace_id = ANY($1) AND workflow = $2 AND version = $3
 ORDER BY created_at DESC LIMIT 1`, workspaceIDs, workflow, version)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return &out[0], nil
}

func (s *PostgresStore) Verdicts(ctx context.Context, workspaceIDs []string, workflow string) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT version, CASE WHEN status = 'pending' THEN 'pending' ELSE verdict END FROM release_reports
 WHERE workspace_id = ANY($1) AND workflow = $2 ORDER BY created_at`, workspaceIDs, workflow)
	if err != nil {
		return nil, fmt.Errorf("postgres: release verdicts: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var version, verdict string
		if err := rows.Scan(&version, &verdict); err != nil {
			return nil, fmt.Errorf("postgres: scan release verdict: %w", err)
		}
		out[version] = verdict
	}
	return out, rows.Err()
}

func (s *PostgresStore) reports(ctx context.Context, sql string, args ...any) ([]Report, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: list release reports: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Report, error) {
		var r Report
		err := row.Scan(&r.WorkspaceID, &r.Workflow, &r.Version, &r.PrevVersion, &r.FirstSeenAt, &r.Status, &r.Verdict,
			&r.Runs, &r.PrevRuns, &r.Findings, &r.Rechecked, &r.CreatedAt, &r.FinalizedAt)
		return r, err
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: scan release report: %w", err)
	}
	if out == nil {
		out = []Report{}
	}
	return out, nil
}

// AddFinding sets the report's finding for f.Metric, replacing an earlier one; a version without a report is left alone.
func (s *PostgresStore) AddFinding(ctx context.Context, workspaceID, workflow, version string, f anomaly.Finding) error {
	_, err := s.pool.Exec(ctx, `UPDATE release_reports SET findings = coalesce((SELECT jsonb_agg(x) FROM jsonb_array_elements(findings) x
 WHERE x->>'metric' <> $4), '[]'::jsonb) || jsonb_build_array($5::jsonb) WHERE workspace_id = $1 AND workflow = $2 AND version = $3`,
		workspaceID, workflow, version, f.Metric, f)
	if err != nil {
		return fmt.Errorf("postgres: add release finding: %w", err)
	}
	return nil
}
