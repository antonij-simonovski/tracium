package alert

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrRuleNotFound = errors.New("alert rule not found")

// Rule is a fixed cost limit for the current UTC day or month. Subject is a
// workflow name or client id, AnySubject for each one, and empty for the workspace.
type Rule struct {
	ID           string    `json:"id"`
	WorkspaceID  string    `json:"workspace_id"`
	Scope        string    `json:"scope"`
	Subject      string    `json:"subject"`
	Period       string    `json:"period"`
	ThresholdUSD float64   `json:"threshold_usd"`
	Enabled      bool      `json:"enabled"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
}

const AnySubject = "*"

// RuleStore persists budget rules, scoped to one workspace.
type RuleStore interface {
	Rules(ctx context.Context, workspaceID string) ([]Rule, error)
	Rule(ctx context.Context, workspaceID, id string) (*Rule, error)
	CreateRule(ctx context.Context, r *Rule) error
	UpdateRule(ctx context.Context, r Rule) error
	DeleteRule(ctx context.Context, workspaceID, id string) error
}

const ruleColumns = `id, workspace_id, scope, subject, period, threshold_usd, enabled, created_by, created_at`

func scanRule(row pgx.Row) (Rule, error) {
	var r Rule
	err := row.Scan(&r.ID, &r.WorkspaceID, &r.Scope, &r.Subject, &r.Period, &r.ThresholdUSD, &r.Enabled, &r.CreatedBy, &r.CreatedAt)
	return r, err
}

func (s *PostgresStore) Rules(ctx context.Context, workspaceID string) ([]Rule, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+ruleColumns+` FROM alert_rules WHERE workspace_id = $1 ORDER BY created_at, id`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list alert rules: %w", err)
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan alert rule: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *PostgresStore) Rule(ctx context.Context, workspaceID, id string) (*Rule, error) {
	r, err := scanRule(s.pool.QueryRow(ctx, `SELECT `+ruleColumns+` FROM alert_rules WHERE id = $1 AND workspace_id = $2`, id, workspaceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRuleNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get alert rule: %w", err)
	}
	return &r, nil
}

func (s *PostgresStore) CreateRule(ctx context.Context, r *Rule) error {
	r.ID = uuid.NewString()
	if err := s.pool.QueryRow(ctx, `INSERT INTO alert_rules (id, workspace_id, scope, subject, period, threshold_usd, enabled, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING created_at`,
		r.ID, r.WorkspaceID, r.Scope, r.Subject, r.Period, r.ThresholdUSD, r.Enabled, r.CreatedBy).Scan(&r.CreatedAt); err != nil {
		return fmt.Errorf("postgres: create alert rule: %w", err)
	}
	return nil
}

func (s *PostgresStore) UpdateRule(ctx context.Context, r Rule) error {
	tag, err := s.pool.Exec(ctx, `UPDATE alert_rules SET threshold_usd = $3, enabled = $4 WHERE id = $1 AND workspace_id = $2`,
		r.ID, r.WorkspaceID, r.ThresholdUSD, r.Enabled)
	if err != nil {
		return fmt.Errorf("postgres: update alert rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRuleNotFound
	}
	return nil
}

func (s *PostgresStore) DeleteRule(ctx context.Context, workspaceID, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM alert_rules WHERE id = $1 AND workspace_id = $2`, id, workspaceID)
	if err != nil {
		return fmt.Errorf("postgres: delete alert rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRuleNotFound
	}
	return nil
}
