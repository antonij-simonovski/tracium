package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	// Import the ClickHouse driver to register it with database/sql.
	_ "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/tracium/api/internal/model"
)

// ClickHouseRepository implements TraceRepository against a real ClickHouse instance.
type ClickHouseRepository struct {
	db *sql.DB
	// queryTimeout bounds how long any single read may run (storage.query_timeout_seconds).
	// It is applied per public method via withTimeout; zero means no bound.
	queryTimeout time.Duration
}

// NewClickHouseRepository opens a connection to ClickHouse using the given DSN.
// queryTimeout bounds each read; pass 0 to leave reads unbounded.
func NewClickHouseRepository(dsn string, queryTimeout time.Duration) (*ClickHouseRepository, error) {
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: open: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("clickhouse: ping: %w", err)
	}
	return &ClickHouseRepository{db: db, queryTimeout: queryTimeout}, nil
}

// withTimeout derives a child context bounded by queryTimeout. Public read
// methods call it once at entry and defer the returned cancel, so the deadline
// covers the whole method — including iterating the *sql.Rows it returns, which
// happens after QueryContext itself returns. A zero timeout leaves ctx as-is.
// The returned cancel is always safe to call.
func (r *ClickHouseRepository) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if r.queryTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, r.queryTimeout)
}

// Ping verifies the ClickHouse connection is alive. Used by the readiness probe.
// It intentionally does not apply queryTimeout — readiness carries its own
// deadline and must not be governed by the read-query budget.
func (r *ClickHouseRepository) Ping(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

// traceSelect aggregates all spans sharing a trace_id into one trace row.
// trace_name follows workflowExpr's precedence (root workflow_name, any
// workflow_name), then the root span's name and service_name so in-flight traces
// still get a sensible name. Aliases must not shadow column names: ClickHouse
// would substitute them back into argMin(name, start_time_ms) and nest aggregates.
const traceSelect = `
SELECT
    trace_id,
    coalesce(nullIf(argMinIf(workflow_name, start_time_ms, workflow_name != '' AND parent_span_id = ''), ''),
             nullIf(argMinIf(workflow_name, start_time_ms, workflow_name != ''), ''),
             nullIf(argMinIf(name, start_time_ms, parent_span_id = ''), ''),
             nullIf(argMinIf(service_name, start_time_ms, service_name != ''), ''),
             argMin(name, start_time_ms)) AS trace_name,
    ` + versionExpr + `                   AS trace_version,
    ` + versionSourceExpr + `             AS trace_version_source,
    any(user_id)                        AS trace_user,
    any(workspace_id)                     AS trace_workspace,
    min(start_time_ms)                    AS started_ms,
    max(end_time_ms)                      AS ended_ms,
    max(end_time_ms) - min(start_time_ms) AS dur_ms,
    toInt64(count())                      AS spans,
    max(` + spanErrored + `)              AS errored,
    sum(cost_usd)                         AS total_cost
FROM ` + tableCalls

// scanTrace reads one aggregated trace row. Works with both *sql.Row and *sql.Rows.
func scanTrace(s interface{ Scan(...any) error }) (model.Trace, error) {
	var t model.Trace
	var hasError uint8
	var spanCount int64
	err := s.Scan(&t.TraceID, &t.Name, &t.WorkflowVersion, &t.VersionSource, &t.UserID, &t.WorkspaceID, &t.StartTimeMs, &t.EndTimeMs,
		&t.DurationMs, &spanCount, &hasError, &t.TotalCostUSD)
	t.HasError = hasError != 0
	t.SpanCount = int(spanCount)
	return t, err
}

// traceFilterSQL renders the WHERE/GROUP BY/HAVING clauses shared by the list
// query and its total-count companion, along with the positional args they bind.
// It stops short of ORDER BY/LIMIT so each caller can append its own tail.
func traceFilterSQL(filter TraceFilter) (string, []any) {
	var args []any

	// Every listing is time-bounded (StartAfter is defaulted in Validate) so the
	// query prunes to its window's granules — the spans table is ordered by
	// start_time_ms — rather than aggregating the whole table on every page load.
	// The query reads tracium.calls (per-call spans only), so metric-derived rows —
	// which carry no trace_id and would otherwise collapse into one phantom trace
	// keyed on '' — are excluded structurally, without a source predicate here.
	clause := "\nWHERE start_time_ms >= ?"
	args = append(args, filter.StartAfter.UnixMilli())
	if !filter.StartBefore.IsZero() {
		clause += " AND start_time_ms < ?"
		args = append(args, filter.StartBefore.UnixMilli())
	}

	// user_id is an optional business filter (the operator's end-client), not
	// an access boundary. Empty means "all clients" — read every trace.
	if filter.UserID != "" {
		clause += " AND user_id = ?"
		args = append(args, filter.UserID)
	}

	// workspace_id scopes the listing to the workspaces the caller may read
	// (their memberships, or the single selected one). Empty matches nothing.
	wsClause, wsArgs := workspaceScope(filter.WorkspaceIDs)
	clause += wsClause
	args = append(args, wsArgs...)
	clause += "\nGROUP BY trace_id"

	var having []string
	if filter.Model != "" {
		having = append(having, "countIf(model = ?) > 0")
		args = append(args, filter.Model)
	}
	// Restrict to one derived workflow (the same derivation the workflows metrics use),
	// so a workflow's "recent runs" list reuses the trace listing. The HAVING runs
	// inside the already time-bounded GROUP BY trace_id — still window-pruned.
	if filter.Workflow != "" {
		having = append(having, workflowExpr+" = ?")
		args = append(args, filter.Workflow)
	}
	if filter.Version != "" {
		having = append(having, versionExpr+" = ?")
		args = append(args, filter.Version)
	}
	if filter.HasError != nil {
		hasError := uint8(0)
		if *filter.HasError {
			hasError = 1
		}
		having = append(having, "max("+spanErrored+") = ?")
		args = append(args, hasError)
	}
	if len(having) > 0 {
		clause += "\nHAVING " + strings.Join(having, " AND ")
	}
	return clause, args
}

// ListTraces returns a page of traces matching the given filter, newest first,
// plus the total number of matching traces across all pages so callers can size
// their pagination UI.
func (r *ClickHouseRepository) ListTraces(ctx context.Context, filter TraceFilter) ([]model.Trace, int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	clause, filterArgs := traceFilterSQL(filter)

	// total counts the trace_id groups matching the filter, ignoring LIMIT/OFFSET.
	var total int64
	totalQ := "SELECT toInt64(count()) FROM (\nSELECT trace_id FROM " + tableCalls + clause + "\n)"
	if err := r.db.QueryRowContext(ctx, totalQ, filterArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("clickhouse: count traces: %w", err)
	}

	q := traceSelect + clause + "\nORDER BY started_ms DESC\nLIMIT ? OFFSET ?"
	args := append(filterArgs, filter.PageSize, (filter.Page-1)*filter.PageSize)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("clickhouse: list traces: %w", err)
	}
	defer rows.Close()

	traces := []model.Trace{}
	for rows.Next() {
		t, err := scanTrace(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("clickhouse: scan trace: %w", err)
		}
		traces = append(traces, t)
	}
	return traces, total, rows.Err()
}

// GetTrace returns a single aggregated trace by its ID.
func (r *ClickHouseRepository) GetTrace(ctx context.Context, traceID string, workspaceIDs []string) (*model.Trace, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	clause, args := traceDetailScope(traceID, workspaceIDs)
	q := traceSelect + clause + "\nGROUP BY trace_id"

	t, err := scanTrace(r.db.QueryRowContext(ctx, q, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("clickhouse: get trace: %w", err)
	}
	return &t, nil
}

var spanSelect = `
SELECT trace_id, span_id, parent_span_id, name, start_time_ms, end_time_ms, duration_ms,
       model, model_normalized, input_tokens, output_tokens, cost_usd, user_id, workspace_id,
       finish_reason, error_type, error_message, schema_version,
       input, output, available_tools, kind, ` + spanIssuesSQL + `
FROM ` + tableCalls

// GetSpans returns all spans for a given trace ID, ordered by start time.
func (r *ClickHouseRepository) GetSpans(ctx context.Context, traceID string, workspaceIDs []string) ([]model.Span, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	clause, args := traceDetailScope(traceID, workspaceIDs)
	rows, err := r.db.QueryContext(ctx, spanSelect+clause+"\nORDER BY start_time_ms", args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: get spans: %w", err)
	}
	defer rows.Close()

	spans := []model.Span{}
	for rows.Next() {
		var s model.Span
		var tools string // available_tools is stored as a JSON string column
		var issues []string
		if err := rows.Scan(&s.TraceID, &s.SpanID, &s.ParentSpanID, &s.Name,
			&s.StartTimeMs, &s.EndTimeMs, &s.DurationMs, &s.Model, &s.ModelNormalized,
			&s.InputTokens, &s.OutputTokens, &s.CostUSD, &s.UserID, &s.WorkspaceID, &s.FinishReason,
			&s.ErrorType, &s.ErrorMessage, &s.SchemaVersion,
			&s.Input, &s.Output, &tools, &s.Kind, &issues); err != nil {
			return nil, fmt.Errorf("clickhouse: scan span: %w", err)
		}
		for _, code := range issues {
			s.SetupIssues = append(s.SetupIssues, setupIssueByCode[code])
		}
		if tools != "" {
			if err := json.Unmarshal([]byte(tools), &s.AvailableTools); err != nil {
				return nil, fmt.Errorf("clickhouse: decode available_tools for span %s: %w", s.SpanID, err)
			}
		}
		spans = append(spans, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Roll up cost and tokens across the whole trace so each parent span reports
	// the totals for its subtree. Cheap: this runs over one trace's spans, not
	// the table.
	if len(spans) == 0 {
		return nil, ErrNotFound
	}
	model.AssignSubtreeTotals(spans)
	return spans, nil
}

// traceDetailScope requires an explicit workspace scope, even when the trace ID
// is known. The same trace ID can occur in multiple workspaces. It reads
// tracium.calls, so metric rows (no trace_id) are excluded structurally — no
// source predicate needed.
func traceDetailScope(traceID string, workspaceIDs []string) (string, []any) {
	clause, scopeArgs := workspaceScope(workspaceIDs)
	return "\nWHERE trace_id = ?" + clause, append([]any{traceID}, scopeArgs...)
}

// Close releases the underlying database pool.
func (r *ClickHouseRepository) Close() error { return r.db.Close() }
