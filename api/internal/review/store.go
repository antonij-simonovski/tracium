package review

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tracium/api/internal/model"
)

var ErrNotFound = errors.New("review not found")

// ErrUserReview is returned when an API review would replace a person's review.
var ErrUserReview = errors.New("a person reviewed this; api reviews cannot replace it")

// ListFilter selects reviews of one workflow: run reviews when Span is empty,
// else reviews of spans with that name. WrongField keeps only reviews in which
// that field was graded wrong. Verdicts empty means any verdict.
type ListFilter struct {
	WorkspaceIDs []string
	Workflow     string
	Span         string
	Version      string
	Verdicts     []string
	WrongField   string
	Page         int
	PageSize     int
}

// Store persists reviews. spanID is "" for the run itself, and span is a span
// name, "" meaning run reviews. Every read is scoped to the caller's
// workspaces; an empty scope matches nothing.
type Store interface {
	Upsert(ctx context.Context, r *model.Review) error
	Get(ctx context.Context, traceID, spanID string, workspaceIDs []string) (*model.Review, error)
	Delete(ctx context.Context, traceID, spanID string, workspaceIDs []string) error
	List(ctx context.Context, f ListFilter) ([]model.Review, int, error)
	Accuracy(ctx context.Context, workspaceIDs []string, workflow string, span *string) ([]model.VersionAccuracy, error)
	FieldAccuracy(ctx context.Context, workspaceIDs []string, workflow, span, version string) ([]model.FieldAccuracy, error)
	Spans(ctx context.Context, workspaceIDs []string, workflow string, version *string) ([]model.ReviewedSpan, error)
	// Reviewed is the subset of traceIDs whose run has a review.
	Reviewed(ctx context.Context, workspaceIDs, traceIDs []string) (map[string]bool, error)
	RunAccuracy(ctx context.Context, workspaceID string) ([]VersionReviews, error)
}

// spanMatch keeps run reviews when $n is empty, else reviews of spans named $n.
func spanMatch(alias string, n int) string {
	p := fmt.Sprintf("$%d", n)
	return fmt.Sprintf("((%[2]s = '' AND %[1]sspan_id = '') OR (%[2]s <> '' AND %[1]sspan_id <> '' AND %[1]sspan_name = %[2]s))", alias, p)
}

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// Upsert replaces the run's review and its graded fields in one transaction.
// An api review never replaces a user's review: that returns ErrUserReview.
func (s *PostgresStore) Upsert(ctx context.Context, r *model.Review) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin review: %w", err)
	}
	defer tx.Rollback(context.Background())

	err = tx.QueryRow(ctx,
		`INSERT INTO run_reviews (workspace_id, trace_id, span_id, span_name, workflow_name, workflow_version, verdict, output, corrected_output, note, reviewed_by, source, reason)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		 ON CONFLICT (workspace_id, trace_id, span_id) DO UPDATE SET
		   verdict = EXCLUDED.verdict, output = EXCLUDED.output, corrected_output = EXCLUDED.corrected_output,
		   note = EXCLUDED.note, reviewed_by = EXCLUDED.reviewed_by, source = EXCLUDED.source, updated_at = NOW(),
		   reason = COALESCE(NULLIF(EXCLUDED.reason, ''), run_reviews.reason)
		 WHERE run_reviews.source = 'api' OR EXCLUDED.source = 'user'
		 RETURNING created_at, updated_at`,
		r.WorkspaceID, r.TraceID, r.SpanID, r.SpanName, r.WorkflowName, r.WorkflowVersion, r.Verdict, r.Output, r.CorrectedOutput, r.Note, r.ReviewedBy, r.Source, r.Reason,
	).Scan(&r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUserReview
	}
	if err != nil {
		return fmt.Errorf("postgres: upsert review: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM run_review_fields WHERE workspace_id = $1 AND trace_id = $2 AND span_id = $3`, r.WorkspaceID, r.TraceID, r.SpanID); err != nil {
		return fmt.Errorf("postgres: clear review fields: %w", err)
	}
	if len(r.Fields) > 0 {
		rows := make([][]any, len(r.Fields))
		for i, f := range r.Fields {
			rows[i] = []any{r.WorkspaceID, r.TraceID, r.SpanID, r.SpanName, r.WorkflowName, r.WorkflowVersion, f.Path, f.Correct, f.Missing}
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"run_review_fields"},
			[]string{"workspace_id", "trace_id", "span_id", "span_name", "workflow_name", "workflow_version", "field_path", "correct", "missing"},
			pgx.CopyFromRows(rows)); err != nil {
			return fmt.Errorf("postgres: insert review fields: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit review: %w", err)
	}
	return nil
}

const reviewColumns = `trace_id, span_id, span_name, workspace_id, workflow_name, workflow_version, verdict, output, corrected_output, note, reviewed_by, source, reason, created_at, updated_at`

func scanReview(row pgx.Row) (model.Review, error) {
	var r model.Review
	err := row.Scan(&r.TraceID, &r.SpanID, &r.SpanName, &r.WorkspaceID, &r.WorkflowName, &r.WorkflowVersion, &r.Verdict,
		&r.Output, &r.CorrectedOutput, &r.Note, &r.ReviewedBy, &r.Source, &r.Reason, &r.CreatedAt, &r.UpdatedAt)
	// An approved output was graded against itself when saved.
	if c := r.CorrectedOutput; c != nil {
		r.Fields, _ = Grade(r.Output, *c)
	} else if r.Verdict == model.VerdictCorrect {
		r.Fields, _ = Grade(r.Output, r.Output)
	}
	if r.Fields == nil {
		r.Fields = []model.ReviewField{}
	}
	return r, err
}

func (s *PostgresStore) Get(ctx context.Context, traceID, spanID string, workspaceIDs []string) (*model.Review, error) {
	r, err := scanReview(s.pool.QueryRow(ctx,
		`SELECT `+reviewColumns+` FROM run_reviews WHERE trace_id = $1 AND span_id = $2 AND workspace_id = ANY($3)
		 ORDER BY updated_at DESC LIMIT 1`, traceID, spanID, workspaceIDs))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get review: %w", err)
	}
	return &r, nil
}

func (s *PostgresStore) Delete(ctx context.Context, traceID, spanID string, workspaceIDs []string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM run_reviews WHERE trace_id = $1 AND span_id = $2 AND workspace_id = ANY($3)`, traceID, spanID, workspaceIDs)
	if err != nil {
		return fmt.Errorf("postgres: delete review: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) List(ctx context.Context, f ListFilter) ([]model.Review, int, error) {
	where := `r.workspace_id = ANY($1) AND r.workflow_name = $2
	  AND ($3 = '' OR r.workflow_version = $3) AND (coalesce(cardinality($4::text[]), 0) = 0 OR r.verdict = ANY($4))
	  AND ($5 = '' OR EXISTS (SELECT 1 FROM run_review_fields f
	        WHERE f.workspace_id = r.workspace_id AND f.trace_id = r.trace_id AND f.span_id = r.span_id AND f.field_path = $5 AND NOT f.correct))
	  AND ` + spanMatch("r.", 6)
	args := []any{f.WorkspaceIDs, f.Workflow, f.Version, f.Verdicts, f.WrongField, f.Span}

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM run_reviews r WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("postgres: count reviews: %w", err)
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+reviewColumns+` FROM run_reviews r WHERE `+where+` ORDER BY r.updated_at DESC LIMIT $7 OFFSET $8`,
		append(args, f.PageSize, (f.Page-1)*f.PageSize)...)
	if err != nil {
		return nil, 0, fmt.Errorf("postgres: list reviews: %w", err)
	}
	defer rows.Close()
	reviews := []model.Review{}
	for rows.Next() {
		r, err := scanReview(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("postgres: scan review: %w", err)
		}
		reviews = append(reviews, r)
	}
	return reviews, total, rows.Err()
}

// Accuracy is per version for the run (span "") or the spans named span; a nil
// span returns the run and every named span, each row tagged with its span.
func (s *PostgresStore) Accuracy(ctx context.Context, workspaceIDs []string, workflow string, span *string) ([]model.VersionAccuracy, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT CASE WHEN span_id = '' THEN '' ELSE span_name END, workflow_version, count(*), count(*) FILTER (WHERE verdict = 'correct')
		   FROM run_reviews WHERE workspace_id = ANY($1) AND workflow_name = $2
		    AND (($3::text IS NULL AND (span_id = '' OR span_name <> '')) OR `+spanMatch("", 3)+`)
		  GROUP BY 1, 2 ORDER BY max(updated_at) DESC`, workspaceIDs, workflow, span)
	if err != nil {
		return nil, fmt.Errorf("postgres: review accuracy: %w", err)
	}
	defer rows.Close()
	out := []model.VersionAccuracy{}
	for rows.Next() {
		var a model.VersionAccuracy
		if err := rows.Scan(&a.Span, &a.Version, &a.ReviewedRuns, &a.CorrectRuns); err != nil {
			return nil, fmt.Errorf("postgres: scan review accuracy: %w", err)
		}
		a.Accuracy = float64(a.CorrectRuns) / float64(a.ReviewedRuns)
		out = append(out, a)
	}
	return out, rows.Err()
}

// Spans lists the span names that have reviews, most reviewed first; a nil
// version means every version.
func (s *PostgresStore) Spans(ctx context.Context, workspaceIDs []string, workflow string, version *string) ([]model.ReviewedSpan, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT span_name, count(*) FROM run_reviews
		  WHERE workspace_id = ANY($1) AND workflow_name = $2 AND ($3::text IS NULL OR workflow_version = $3) AND span_id <> '' AND span_name <> ''
		  GROUP BY span_name ORDER BY count(*) DESC, span_name`, workspaceIDs, workflow, version)
	if err != nil {
		return nil, fmt.Errorf("postgres: reviewed spans: %w", err)
	}
	defer rows.Close()
	out := []model.ReviewedSpan{}
	for rows.Next() {
		var sp model.ReviewedSpan
		if err := rows.Scan(&sp.Name, &sp.Reviewed); err != nil {
			return nil, fmt.Errorf("postgres: scan reviewed span: %w", err)
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

func (s *PostgresStore) FieldAccuracy(ctx context.Context, workspaceIDs []string, workflow, span, version string) ([]model.FieldAccuracy, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT field_path, count(*), count(*) FILTER (WHERE NOT correct), count(*) FILTER (WHERE missing)
		   FROM run_review_fields WHERE workspace_id = ANY($1) AND workflow_name = $2 AND workflow_version = $3 AND `+spanMatch("", 4)+`
		  GROUP BY field_path
		  ORDER BY count(*) FILTER (WHERE correct)::float / count(*), field_path`, workspaceIDs, workflow, version, span)
	if err != nil {
		return nil, fmt.Errorf("postgres: field accuracy: %w", err)
	}
	defer rows.Close()
	out := []model.FieldAccuracy{}
	for rows.Next() {
		var a model.FieldAccuracy
		if err := rows.Scan(&a.Path, &a.Reviewed, &a.Wrong, &a.Missing); err != nil {
			return nil, fmt.Errorf("postgres: scan field accuracy: %w", err)
		}
		a.Accuracy = float64(a.Reviewed-a.Wrong) / float64(a.Reviewed)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *PostgresStore) Reviewed(ctx context.Context, workspaceIDs, traceIDs []string) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT trace_id FROM run_reviews WHERE workspace_id = ANY($1) AND trace_id = ANY($2) AND span_id = ''`, workspaceIDs, traceIDs)
	if err != nil {
		return nil, fmt.Errorf("postgres: reviewed runs: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("postgres: scan reviewed runs: %w", err)
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// RunAccuracy counts the workspace's run reviews per workflow version, each
// workflow's versions in the order they were first reviewed.
func (s *PostgresStore) RunAccuracy(ctx context.Context, workspaceID string) ([]VersionReviews, error) {
	rows, err := s.pool.Query(ctx, `SELECT workflow_name, workflow_version, count(*), count(*) FILTER (WHERE verdict = 'correct'),
		  count(*) FILTER (WHERE reason IN ('', 'sample')), count(*) FILTER (WHERE reason IN ('', 'sample') AND verdict = 'correct'),
		  min(created_at), max(updated_at)
		 FROM run_reviews WHERE workspace_id = $1 AND span_id = ''
		GROUP BY 1, 2 ORDER BY 1, min(created_at), 2`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("postgres: run accuracy: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (VersionReviews, error) {
		var v VersionReviews
		err := row.Scan(&v.Workflow, &v.Version, &v.Reviews, &v.Correct, &v.Unflagged, &v.UnflaggedCorrect, &v.FirstAt, &v.LastAt)
		return v, err
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: scan run accuracy: %w", err)
	}
	return out, nil
}
