package query

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/tracium/api/internal/model"
)

// The two ingestion relations, both views over tracium.spans filtered by source,
// so queries pick a relation instead of adding a source predicate.
const (
	tableCalls        = "tracium.calls"
	tableUsageMetrics = "tracium.usage_metrics"
)

// workspaceScope renders the workspace access clause for the caller's allowed
// workspaces. An empty set matches nothing, never everything.
func workspaceScope(workspaces []string) (string, []any) {
	switch len(workspaces) {
	case 0:
		return " AND 1 = 0", nil
	case 1:
		return " AND workspace_id = ?", []any{workspaces[0]}
	default:
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(workspaces)), ",")
		args := make([]any, len(workspaces))
		for i, w := range workspaces {
			args[i] = w
		}
		return " AND workspace_id IN (" + placeholders + ")", args
	}
}

// window builds the time-range, user and workspace WHERE clause shared by every
// metrics query. lo/hi are epoch-millis bounds; an empty user means no filter.
func window(user string, workspaces []string, lo, hi int64) (string, []any) {
	clause := "start_time_ms >= ? AND start_time_ms < ?"
	args := []any{lo, hi}
	if user != "" {
		clause += " AND user_id = ?"
		args = append(args, user)
	}
	wsClause, wsArgs := workspaceScope(workspaces)
	clause += wsClause
	args = append(args, wsArgs...)
	return clause, args
}

// reconciledCostUnion builds the row-level UNION ALL over the two cost relations
// for one window: calls rows contribute span_cost, usage_metrics rows
// metric_cost. The returned args bind the clause once per branch.
//
// Callers reconcile per bucket with greatest, not sum: spans and metrics meter
// the same spend and either may be incomplete (sampling, streamed calls without
// usage, partial metric coverage), so each is a lower bound and the greater is
// the tightest estimate that never double-counts.
func reconciledCostUnion(clause string, args []any) (string, []any) {
	sql := fmt.Sprintf(`SELECT start_time_ms, cost_usd AS span_cost, 0 AS metric_cost FROM %s WHERE %s
    UNION ALL
    SELECT start_time_ms, 0 AS span_cost, cost_usd AS metric_cost FROM %s WHERE %s`,
		tableCalls, clause, tableUsageMetrics, clause)
	both := make([]any, 0, len(args)*2)
	both = append(both, args...)
	both = append(both, args...)
	return sql, both
}

// costTotal sums reconciled cost over [lo, hi), on the same bucket grid the cost
// series uses so the headline KPI always equals the sum of the chart. Cost is
// reconciled per bucket across both relations (see reconciledCostUnion); summing
// the per-bucket greatest matches CostSeries bucket-for-bucket.
func (r *ClickHouseRepository) costTotal(ctx context.Context, user string, workspaces []string, lo, hi, bucketMs int64) (float64, error) {
	clause, args := window(user, workspaces, lo, hi)
	union, uargs := reconciledCostUnion(clause, args)
	var cost float64
	// The GROUP BY bucket placeholder is textually after the union, so bucketMs
	// binds last.
	q := fmt.Sprintf(`
SELECT sum(bucket_cost)
FROM (
    SELECT greatest(sum(span_cost), sum(metric_cost)) AS bucket_cost
    FROM (%s)
    GROUP BY intDiv(start_time_ms, ?)
)`, union)
	if err := r.db.QueryRowContext(ctx, q, append(uargs, bucketMs)...).Scan(&cost); err != nil {
		return 0, fmt.Errorf("clickhouse: cost total: %w", err)
	}
	return sanitize(cost), nil
}

// traceDurationExpr defines a run's end-to-end latency: the span of wall-clock
// time from the first span starting to the last span ending. Both the headline
// LatencyP95 KPI (kpiAggregates) and the latency chart (LatencySeries) build on
// this single definition so the two always measure the same thing.
const traceDurationExpr = `max(end_time_ms) - min(start_time_ms)`

// spanErrored is the single definition of a failed span: either a typed error
// or a bare error message. The frontend marks a span failed on the same OR
// (error_type || error_message), so every trace-level rollup that decides
// whether a trace errored uses this to keep the status pill, the error banner,
// the failed-span counts, and the error-rate/failures metrics all agreeing.
const spanErrored = `(error_type != '' OR error_message != '')`

// kpiAggregates rolls every span in the window up to its trace, then reduces the
// traces to the trace-shaped headline numbers. Cost is computed separately (it
// may come from the metric relation), so it is not in this query — it reads
// tracium.calls, whose rows are the per-call spans that carry trace identity,
// durations, and errors.
const kpiAggregates = `
SELECT
    toInt64(count())     AS runs,
    quantile(0.95)(dur)  AS p95,
    avg(errored)         AS err_rate
FROM (
    SELECT
        ` + traceDurationExpr + ` AS dur,
        max(` + spanErrored + `)  AS errored
    FROM ` + tableCalls + `
    WHERE %s
    GROUP BY trace_id
)`

// kpiRow holds one window's aggregates before they are paired into deltas.
type kpiRow struct {
	cost, p95, errRate float64
	runs               int64
}

// kpiWindow computes one window's headline numbers: runs/latency/errors from
// the per-call spans (tracium.calls), and cost reconciled across both relations
// (see reconciledCostUnion).
func (r *ClickHouseRepository) kpiWindow(ctx context.Context, f MetricsFilter, lo, hi int64) (kpiRow, error) {
	clause, args := window(f.UserID, f.WorkspaceIDs, lo, hi)
	var row kpiRow
	err := r.db.QueryRowContext(ctx, fmt.Sprintf(kpiAggregates, clause), args...).
		Scan(&row.runs, &row.p95, &row.errRate)
	if err != nil {
		return kpiRow{}, fmt.Errorf("clickhouse: kpi window: %w", err)
	}
	// Over an empty window quantile and avg scan as NaN; coerce so deltas and
	// JSON encoding stay well-defined.
	row.p95 = sanitize(row.p95)
	row.errRate = sanitize(row.errRate)

	cost, err := r.costTotal(ctx, f.UserID, f.WorkspaceIDs, lo, hi, f.Bucket.Milliseconds())
	if err != nil {
		return kpiRow{}, err
	}
	row.cost = cost
	return row, nil
}

// OverviewKPIs computes the four headline KPIs for the current window alongside
// their change versus the equal-length preceding window. Cost reconciles both
// ingestion relations per bucket (reconciledCostUnion) on the same grid in both
// sub-windows and in CostSeries, so the delta compares like with like and the
// KPI always equals the sum of the chart.
func (r *ClickHouseRepository) OverviewKPIs(ctx context.Context, f MetricsFilter) (model.KPISet, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if f.UseRollup() {
		return r.overviewKPIsRollup(ctx, f)
	}
	cur, err := r.kpiWindow(ctx, f, f.Start.UnixMilli(), f.End.UnixMilli())
	if err != nil {
		return model.KPISet{}, err
	}
	prev, err := r.kpiWindow(ctx, f, f.PrevStart.UnixMilli(), f.Start.UnixMilli())
	if err != nil {
		return model.KPISet{}, err
	}
	return model.KPISet{
		Cost:       kpi(cur.cost, prev.cost, true),
		Runs:       kpi(float64(cur.runs), float64(prev.runs), false),
		LatencyP95: kpi(cur.p95, prev.p95, true),
		ErrorRate:  kpi(cur.errRate, prev.errRate, true),
	}, nil
}

// bucketAxis returns the contiguous bucket grid spanning the window: the
// bucket-aligned start of the first slot (base), the bucket width, and the
// number of slots up to (but not including) End. A series scans its real buckets
// into a map keyed by bucket_ms, then walks base, base+bucketMs, … to materialise
// a gap-free axis, zero-filling slots with no data. The window clause pins every
// real bucket onto this same grid, so map lookups line up exactly.
func bucketAxis(f MetricsFilter) (base, bucketMs int64, n int) {
	bucketMs = f.Bucket.Milliseconds()
	base = (f.Start.UnixMilli() / bucketMs) * bucketMs
	n = int((f.End.UnixMilli() - base + bucketMs - 1) / bucketMs)
	if n < 1 {
		n = 1
	}
	return base, bucketMs, n
}

// snapWindow rounds a window's end down to grid (preserving its length) so that
// requests arriving within the same grid interval produce a byte-identical query
// — the precondition for ClickHouse's query cache to hit. User and bucket are
// unchanged. The caller trades up-to-grid staleness for cross-request caching.
func snapWindow(f MetricsFilter, grid time.Duration) MetricsFilter {
	span := f.End.Sub(f.Start)
	f.End = f.End.Truncate(grid)
	f.Start = f.End.Add(-span)
	return f
}

// CostSeries returns total cost per time bucket across the window, zero-filled
// over the full window so quiet buckets read 0 spend rather than being dropped —
// the sparkline and bar chart then show the true shape.
func (r *ClickHouseRepository) CostSeries(ctx context.Context, f MetricsFilter) ([]model.CostPoint, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if f.UseRollup() {
		return r.costSeriesRollup(ctx, f)
	}
	bucketMs := f.Bucket.Milliseconds()

	// Workflow- or version-scoped cost must use per-call spans (the metric relation
	// carries no workflow identity) and roll up to traces so the filter can apply.
	// The unfiltered path reconciles both relations instead.
	if f.Workflow != "" || f.Version != "" {
		clause, args := window(f.UserID, f.WorkspaceIDs, f.Start.UnixMilli(), f.End.UnixMilli())
		scopeCols, scopeWhere, scopeArgs := traceScope(f)
		q := fmt.Sprintf(`
SELECT intDiv(ts, ?) * ? AS bucket_ms, sum(cost) AS value
FROM (
    SELECT
        min(start_time_ms) AS ts,
        sum(cost_usd)      AS cost%s
    FROM %s
    WHERE %s
    GROUP BY trace_id
)%s
GROUP BY bucket_ms`, scopeCols, tableCalls, clause, scopeWhere)
		qArgs := append([]any{bucketMs, bucketMs}, args...)
		points, err := bucketSeries(ctx, r.db, f, q, append(qArgs, scopeArgs...), scanCostBucket, costPoint)
		if err != nil {
			return nil, fmt.Errorf("clickhouse: cost series (workflow): %w", err)
		}
		return points, nil
	}

	// Reconcile the two cost relations per bucket (see reconciledCostUnion) — the
	// same grid and reconciliation as the KPI, so chart and headline agree. The
	// bucket placeholders are in the outer SELECT/GROUP BY, textually before the
	// union, so bucketMs binds first.
	clause, args := window(f.UserID, f.WorkspaceIDs, f.Start.UnixMilli(), f.End.UnixMilli())
	union, uargs := reconciledCostUnion(clause, args)
	q := fmt.Sprintf(`
SELECT intDiv(start_time_ms, ?) * ? AS bucket_ms, greatest(sum(span_cost), sum(metric_cost)) AS value
FROM (%s)
GROUP BY bucket_ms`, union)

	points, err := bucketSeries(ctx, r.db, f, q, append([]any{bucketMs, bucketMs}, uargs...), scanCostBucket, costPoint)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: cost series: %w", err)
	}
	return points, nil
}

// LatencySeries returns p50/p95/p99 of end-to-end run (trace) latency per time
// bucket, in ms. Each trace is first rolled up to its total duration
// (max(end_time_ms) - min(start_time_ms)) and assigned to the bucket of its
// start, then the quantiles run over trace durations — matching the headline
// LatencyP95 KPI (see kpiAggregates) so the chart and KPI measure the same thing.
//
// The series is null-filled over the full window (see bucketAxis): every slot is
// present so the x-axis aligns with the cost chart beside it, but quiet buckets
// carry nil percentiles rather than 0. Latency is undefined when nothing ran, so
// the chart draws a genuine gap instead of a dip to the floor.
func (r *ClickHouseRepository) LatencySeries(ctx context.Context, f MetricsFilter) ([]model.LatencyPoint, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if f.UseRollup() {
		// Trace-duration percentiles can't be derived from the daily rollup, and
		// scanning a quarter/year of raw spans for them is exactly what we avoid.
		// Return a null-filled axis; the dashboard hides latency for long windows.
		return latencySeriesEmpty(f), nil
	}
	clause, args := window(f.UserID, f.WorkspaceIDs, f.Start.UnixMilli(), f.End.UnixMilli())
	scopeCols, scopeWhere, scopeArgs := traceScope(f)
	base, bucketMs, n := bucketAxis(f)
	q := fmt.Sprintf(`
SELECT
    bucket_ms,
    quantile(0.5)(dur)  AS p50,
    quantile(0.95)(dur) AS p95,
    quantile(0.99)(dur) AS p99
FROM (
    SELECT
        intDiv(min(start_time_ms), ?) * ? AS bucket_ms,
        `+traceDurationExpr+` AS dur%s
    FROM `+tableCalls+`
    WHERE %s
    GROUP BY trace_id
)%s
GROUP BY bucket_ms`, scopeCols, clause, scopeWhere)

	qArgs := append([]any{bucketMs, bucketMs}, args...)
	rows, err := r.db.QueryContext(ctx, q, append(qArgs, scopeArgs...)...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: latency series: %w", err)
	}
	defer rows.Close()

	type quantiles struct{ p50, p95, p99 float64 }
	byBucket := make(map[int64]quantiles)
	for rows.Next() {
		var bm int64
		var q quantiles
		if err := rows.Scan(&bm, &q.p50, &q.p95, &q.p99); err != nil {
			return nil, fmt.Errorf("clickhouse: scan latency point: %w", err)
		}
		byBucket[bm] = q
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	points := make([]model.LatencyPoint, n)
	for i := 0; i < n; i++ {
		bm := base + int64(i)*bucketMs
		points[i] = model.LatencyPoint{BucketMs: bm}
		if q, ok := byBucket[bm]; ok {
			points[i].P50, points[i].P95, points[i].P99 = &q.p50, &q.p95, &q.p99
		}
	}
	return points, nil
}

// ErrorSeries returns failed-vs-total run counts per time bucket across the
// window — the data behind the "Failures by day" strip and the Traces / Failure
// rate sparklines. Runs (traces) are bucketed by when they started; a run counts
// as errored if any of its spans carries an error type. The series is zero-filled
// over the full window so quiet buckets read 0 runs rather than being dropped.
func (r *ClickHouseRepository) ErrorSeries(ctx context.Context, f MetricsFilter) ([]model.ErrorPoint, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if f.UseRollup() {
		return r.errorSeriesRollup(ctx, f)
	}
	clause, args := window(f.UserID, f.WorkspaceIDs, f.Start.UnixMilli(), f.End.UnixMilli())
	scopeCols, scopeWhere, scopeArgs := traceScope(f)
	bucketMs := f.Bucket.Milliseconds()
	q := fmt.Sprintf(`
SELECT
    bucket_ms,
    toInt64(countIf(errored)) AS errors,
    toInt64(count())          AS total
FROM (
    SELECT
        intDiv(min(start_time_ms), ?) * ? AS bucket_ms,
        max(`+spanErrored+`)              AS errored%s
    FROM `+tableCalls+`
    WHERE %s
    GROUP BY trace_id
)%s
GROUP BY bucket_ms`, scopeCols, clause, scopeWhere)

	qArgs := append([]any{bucketMs, bucketMs}, args...)
	points, err := bucketSeries(ctx, r.db, f, q, append(qArgs, scopeArgs...), scanErrorBucket, errorPoint)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: error series: %w", err)
	}
	return points, nil
}

// TokenSeries returns input and output tokens per time bucket, zero-filled over
// the window. Tokens come from per-call spans only, the same source as the
// model-costs totals behind the Tokens KPI, so the chart sums to that figure.
// Spans roll up to traces (bucketed at the trace start) so the workflow and
// version filters apply the same way they do for the error series.
func (r *ClickHouseRepository) TokenSeries(ctx context.Context, f MetricsFilter) ([]model.TokenPoint, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if f.UseRollup() {
		return r.tokenSeriesRollup(ctx, f)
	}
	clause, args := window(f.UserID, f.WorkspaceIDs, f.Start.UnixMilli(), f.End.UnixMilli())
	scopeCols, scopeWhere, scopeArgs := traceScope(f)
	bucketMs := f.Bucket.Milliseconds()
	q := fmt.Sprintf(`
SELECT
    bucket_ms,
    toInt64(sum(input))  AS input_tokens,
    toInt64(sum(output)) AS output_tokens
FROM (
    SELECT
        intDiv(min(start_time_ms), ?) * ? AS bucket_ms,
        sum(input_tokens)                 AS input,
        sum(output_tokens)                AS output%s
    FROM `+tableCalls+`
    WHERE %s
    GROUP BY trace_id
)%s
GROUP BY bucket_ms`, scopeCols, clause, scopeWhere)

	qArgs := append([]any{bucketMs, bucketMs}, args...)
	points, err := bucketSeries(ctx, r.db, f, q, append(qArgs, scopeArgs...), scanTokenBucket, tokenPoint)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: token series: %w", err)
	}
	return points, nil
}

// workflowExpr derives a trace's workflow inside a `GROUP BY trace_id` subquery:
// the root span's workflow_name, else the earliest span's workflow_name, else
// service_name, else the earliest span's name. Preferring the root avoids
// landing on an auto-instrumented child that starts in the same millisecond.
const workflowExpr = `coalesce(` +
	`nullIf(argMinIf(workflow_name, start_time_ms, workflow_name != '' AND parent_span_id = ''), ''), ` +
	`nullIf(argMinIf(workflow_name, start_time_ms, workflow_name != ''), ''), ` +
	`nullIf(argMinIf(service_name, start_time_ms, service_name != ''), ''), ` +
	`argMin(name, start_time_ms))`

// versionExpr and versionSourceExpr take a trace's version from its root span,
// else its earliest versioned span: false sorts first, so root spans win.
const (
	versionExpr       = `argMinIf(workflow_version, (parent_span_id != '', start_time_ms), workflow_version != '')`
	versionSourceExpr = `argMinIf(version_source, (parent_span_id != '', start_time_ms), workflow_version != '')`
)

// traceScope returns the inner columns, outer WHERE and args for the workflow and version filters.
func traceScope(f MetricsFilter) (cols, where string, args []any) {
	var conds []string
	if f.Workflow != "" {
		cols += ",\n        " + workflowExpr + " AS name"
		conds = append(conds, "name = ?")
		args = append(args, f.Workflow)
	}
	if f.Version != "" {
		cols += ",\n        " + versionExpr + " AS version"
		conds = append(conds, "version = ?")
		args = append(args, f.Version)
	}
	if len(conds) > 0 {
		where = "\nWHERE " + strings.Join(conds, " AND ")
	}
	return cols, where, args
}

// TopWorkflows returns the highest-spending workflows in the window. A workflow is the
// trace's derived workflow (see workflowExpr).
func (r *ClickHouseRepository) TopWorkflows(ctx context.Context, f MetricsFilter, limit int) ([]model.WorkflowCost, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if f.UseRollup() {
		return r.topWorkflowsRollup(ctx, f, limit)
	}
	clause, args := window(f.UserID, f.WorkspaceIDs, f.Start.UnixMilli(), f.End.UnixMilli())
	q := fmt.Sprintf(`
SELECT name, sum(cost) AS cost, toInt64(count()) AS calls
FROM (
    SELECT
        %s AS name,
        sum(cost_usd) AS cost
    FROM `+tableCalls+`
    WHERE %s
    GROUP BY trace_id
)
GROUP BY name
ORDER BY cost DESC
LIMIT ?`, workflowExpr, clause)

	rows, err := r.db.QueryContext(ctx, q, append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: top workflows: %w", err)
	}
	defer rows.Close()

	workflows := []model.WorkflowCost{}
	for rows.Next() {
		var a model.WorkflowCost
		if err := rows.Scan(&a.Name, &a.Cost, &a.Calls); err != nil {
			return nil, fmt.Errorf("clickhouse: scan workflow: %w", err)
		}
		workflows = append(workflows, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Each workflow's per-bucket call count, on the shared zero-filled axis.
	bucketMs := f.Bucket.Milliseconds()
	trendQ := fmt.Sprintf(`
SELECT name, bucket_ms, toInt64(count()) AS calls
FROM (
    SELECT
        %s AS name,
        intDiv(min(start_time_ms), ?) * ? AS bucket_ms
    FROM `+tableCalls+`
    WHERE %s
    GROUP BY trace_id
)
GROUP BY name, bucket_ms`, workflowExpr, clause)
	if err := fillWorkflowTrends(ctx, r.db, f, workflows, workflowCostTrend, trendQ, append([]any{bucketMs, bucketMs}, args...)); err != nil {
		return nil, fmt.Errorf("clickhouse: workflow trends: %w", err)
	}
	return workflows, nil
}

// workflowsCacheGrid snaps the window's end to a coarse grid so repeated requests
// for the same range (across users, or one user polling) produce a byte-identical
// query that ClickHouse's query cache can serve, instead of re-scanning the
// window every time. The grid width also bounds how stale the result can be.
const workflowsCacheGrid = time.Minute

// ListWorkflows returns the most active workflows in the window for the Workflows page:
// per workflow its run count, total spend, mean run latency, error rate, last trace,
// and a per-bucket call-count sparkline. A workflow is the trace's derived workflow
// (see workflowExpr).
//
// Two efficiency measures (the page is read often, by many dashboards at once):
//   - Single scan: the per-workflow aggregates and the sparkline are computed in one
//     pass. The inner query rolls spans up to traces; the outer reduces traces to
//     workflows and folds the per-bucket counts into a map via sumMap, which Go then
//     expands onto the shared zero-filled axis. (TopWorkflows still uses two scans.)
//   - Cached: the window is snapped to workflowsCacheGrid and the read opts into the
//     query cache, so concurrent identical requests collapse to one scan.
func (r *ClickHouseRepository) ListWorkflows(ctx context.Context, f MetricsFilter, limit int) ([]model.Workflow, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if f.UseRollup() {
		return r.listWorkflowsRollup(ctx, f, limit)
	}
	cf := snapWindow(f, workflowsCacheGrid)
	clause, args := window(cf.UserID, cf.WorkspaceIDs, cf.Start.UnixMilli(), cf.End.UnixMilli())
	bucketMs := cf.Bucket.Milliseconds()
	q := fmt.Sprintf(`
SELECT
    name,
    toInt64(count())     AS calls,
    sum(cost)            AS cost,
    avg(dur)             AS avg_latency_ms,
    avg(errored)         AS error_rate,
    argMax(trace_id, ts) AS last_trace_id,
    CAST(sumMap([bucket], [toInt64(1)]), 'Map(Int64, Int64)') AS trend
FROM (
    SELECT
        trace_id,
        %s AS name,
        sum(cost_usd)        AS cost,
        `+traceDurationExpr+` AS dur,
        max(`+spanErrored+`) AS errored,
        min(start_time_ms)   AS ts,
        intDiv(min(start_time_ms), ?) * ? AS bucket
    FROM `+tableCalls+`
    WHERE %s
    GROUP BY trace_id
)
GROUP BY name
ORDER BY calls DESC
LIMIT ?
SETTINGS use_query_cache = 1, query_cache_ttl = 60`, workflowExpr, clause)

	qArgs := append([]any{bucketMs, bucketMs}, args...)
	qArgs = append(qArgs, limit)
	rows, err := r.db.QueryContext(ctx, q, qArgs...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: list workflows: %w", err)
	}
	defer rows.Close()

	workflows := []model.Workflow{}
	for rows.Next() {
		var a model.Workflow
		var trend map[int64]int64
		if err := rows.Scan(&a.Name, &a.Calls, &a.Cost, &a.AvgLatencyMs, &a.ErrorRate, &a.LastTraceID, &trend); err != nil {
			return nil, fmt.Errorf("clickhouse: scan workflow: %w", err)
		}
		a.Cost, a.AvgLatencyMs, a.ErrorRate = sanitize(a.Cost), sanitize(a.AvgLatencyMs), sanitize(a.ErrorRate)
		a.Trend = zeroFillTrend(cf, trend)
		workflows = append(workflows, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return workflows, nil
}

// WorkflowDetail returns one workflow's headline metrics for the detail page (f.Workflow
// names it), plus its current version and tool surface. Span-backed only: there
// is no workflow-config store, so runtime params are not served. The window is bounded
// like every other metric; callers keep it off the rollup (the handler rejects
// rollup ranges) because per-workflow latency can't come from the daily rollup.
// Returns ErrNotFound when the workflow has no runs in the window.
//
// The inner query rolls spans up to traces (cost, duration, errored, tokens,
// model, start) and the outer reduces the workflow's traces to the headline
// numbers — the same two-level shape as ListWorkflows, filtered to one workflow.
func (r *ClickHouseRepository) WorkflowDetail(ctx context.Context, f MetricsFilter) (model.WorkflowDetail, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	clause, args := window(f.UserID, f.WorkspaceIDs, f.Start.UnixMilli(), f.End.UnixMilli())
	q := fmt.Sprintf(`
SELECT
    toInt64(count())                 AS calls,
    sum(cost)                        AS cost,
    avg(dur)                         AS avg_latency_ms,
    quantile(0.95)(dur)              AS p95_latency_ms,
    avg(errored)                     AS error_rate,
    toInt64(sum(in_tok))             AS input_tokens,
    toInt64(sum(out_tok))            AS output_tokens,
    topKIf(1)(model, model != '')[1] AS model,
    argMax(trace_id, ts)             AS last_trace_id,
    argMaxIf(version, ts, version != '') AS current_version,
    argMaxIf(src, ts, version != '')     AS current_version_source
FROM (
    SELECT
        trace_id,
        %s AS name,
        %s AS version,
        %s AS src,
        argMin(%s, start_time_ms) AS model,
        sum(cost_usd)        AS cost,
        `+traceDurationExpr+` AS dur,
        max(`+spanErrored+`) AS errored,
        sum(input_tokens)    AS in_tok,
        sum(output_tokens)   AS out_tok,
        min(start_time_ms)   AS ts
    FROM `+tableCalls+`
    WHERE %s
    GROUP BY trace_id
)
WHERE name = ?`, workflowExpr, versionExpr, versionSourceExpr, modelExpr, clause)

	var (
		d         model.WorkflowDetail
		p95       float64
		modelName string
		lastTrace string
	)
	err := r.db.QueryRowContext(ctx, q, append(args, f.Workflow)...).Scan(
		&d.Calls, &d.Cost, &d.AvgLatencyMs, &p95, &d.ErrorRate,
		&d.InputTokens, &d.OutputTokens, &modelName, &lastTrace, &d.CurrentVersion, &d.CurrentVersionSource)
	if err != nil {
		return model.WorkflowDetail{}, fmt.Errorf("clickhouse: workflow detail: %w", err)
	}
	// The outer aggregate always returns one row; a workflow with no runs in the
	// window comes back as zero calls, which is a 404, not an empty detail.
	if d.Calls == 0 {
		return model.WorkflowDetail{}, ErrNotFound
	}

	d.Name = f.Workflow
	d.Cost, d.AvgLatencyMs, d.ErrorRate = sanitize(d.Cost), sanitize(d.AvgLatencyMs), sanitize(d.ErrorRate)
	if s := sanitize(p95); s > 0 {
		d.P95LatencyMs = &s
	}
	d.Model = modelName
	d.Provider = providerFromModel(modelName)
	d.LastTraceID = lastTrace

	// The tool surface is the union of available_tools across the workflow's most
	// recent run — cheap (one trace, bloom-indexed by trace_id) and representative
	// of the current deployment. Empty when content/tool capture is off.
	if lastTrace != "" {
		spans, err := r.GetSpans(ctx, lastTrace, f.WorkspaceIDs)
		if err != nil {
			return model.WorkflowDetail{}, fmt.Errorf("clickhouse: workflow detail tools: %w", err)
		}
		d.Tools = unionTools(spans)
	}
	return d, nil
}

// providerFromModel infers a model's provider from its id prefix. Best-effort:
// returns "" when unrecognised, and the dashboard then omits the provider row.
func providerFromModel(m string) string {
	switch {
	case strings.HasPrefix(m, "claude"):
		return "anthropic"
	case strings.HasPrefix(m, "gpt"), strings.HasPrefix(m, "o1"), strings.HasPrefix(m, "o3"):
		return "openai"
	case strings.HasPrefix(m, "gemini"):
		return "google"
	default:
		return ""
	}
}

// unionTools collapses available_tools across a trace's spans into one entry per
// tool name, marked used if any span used it and taking the first non-empty
// description. Order follows first appearance so the list is stable.
func unionTools(spans []model.Span) []model.AvailableTool {
	var out []model.AvailableTool
	idx := make(map[string]int)
	for _, s := range spans {
		for _, t := range s.AvailableTools {
			if i, ok := idx[t.Name]; ok {
				if t.Used {
					out[i].Used = true
				}
				if out[i].Description == "" {
					out[i].Description = t.Description
				}
				continue
			}
			idx[t.Name] = len(out)
			out = append(out, t)
		}
	}
	return out
}

// Failures returns the workflows with the most errored runs in the window, plus
// the total number of failed runs across all workflows.
func (r *ClickHouseRepository) Failures(ctx context.Context, f MetricsFilter, limit int) ([]model.Failure, int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if f.UseRollup() {
		return r.failuresRollup(ctx, f, limit)
	}
	clause, args := window(f.UserID, f.WorkspaceIDs, f.Start.UnixMilli(), f.End.UnixMilli())
	// Per trace: its workflow (see workflowExpr), whether it errored, and one error
	// type. Grouped by workflow we get failed vs total runs and the most common error.
	q := fmt.Sprintf(`
SELECT
    name                                AS workflow,
    toInt64(countIf(errored))           AS failed,
    toInt64(count())                    AS runs,
    topKIf(1)(err, errored != 0)[1]     AS top_error
FROM (
    SELECT
        %s AS name,
        max(`+spanErrored+`)                AS errored,
        anyIf(error_type, error_type != '') AS err
    FROM `+tableCalls+`
    WHERE %s
    GROUP BY trace_id
)
GROUP BY workflow
HAVING failed > 0
ORDER BY failed DESC
LIMIT ?`, workflowExpr, clause)

	rows, err := r.db.QueryContext(ctx, q, append(args, limit)...)
	if err != nil {
		return nil, 0, fmt.Errorf("clickhouse: failures: %w", err)
	}
	defer rows.Close()

	failures := []model.Failure{}
	var total int64
	for rows.Next() {
		var fl model.Failure
		var runs int64
		if err := rows.Scan(&fl.Workflow, &fl.Count, &runs, &fl.TopError); err != nil {
			return nil, 0, fmt.Errorf("clickhouse: scan failure: %w", err)
		}
		if runs > 0 {
			fl.Pct = float64(fl.Count) / float64(runs)
		}
		failures = append(failures, fl)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	// LIMIT only caps the listed workflows, so total errored runs is a separate count.
	totalQ := fmt.Sprintf(`
SELECT toInt64(count())
FROM (
    SELECT trace_id FROM `+tableCalls+`
    WHERE %s
    GROUP BY trace_id
    HAVING max(`+spanErrored+`) = 1
)`, clause)
	if err := r.db.QueryRowContext(ctx, totalQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("clickhouse: failures total: %w", err)
	}
	return failures, total, nil
}

// modelExpr picks a span's model, preferring the normalized id and falling back
// to the raw model for rows written before normalization existed.
const modelExpr = `if(model_normalized != '', model_normalized, model)`

// ModelCosts returns the highest-spending models in the window for the usage
// page's "Where it goes" list. Unlike the trace-rollup metrics, this aggregates
// at the span level: a model is attributed cost and tokens directly from the
// spans that named it, and Calls counts those model invocations. Spans with no
// model (tool/internal spans) carry no model and are excluded.
func (r *ClickHouseRepository) ModelCosts(ctx context.Context, f MetricsFilter, limit int) ([]model.ModelCost, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if f.UseRollup() {
		return r.modelCostsRollup(ctx, f, limit)
	}
	clause, args := window(f.UserID, f.WorkspaceIDs, f.Start.UnixMilli(), f.End.UnixMilli())
	q := fmt.Sprintf(`
SELECT
    %s                 AS name,
    sum(cost_usd)      AS cost,
    toInt64(count())   AS calls,
    toInt64(sum(input_tokens))  AS input_tokens,
    toInt64(sum(output_tokens)) AS output_tokens
FROM `+tableCalls+`
WHERE %s AND %s != ''
GROUP BY name
ORDER BY cost DESC
LIMIT ?`, modelExpr, clause, modelExpr)

	rows, err := r.db.QueryContext(ctx, q, append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: model costs: %w", err)
	}
	defer rows.Close()

	models := []model.ModelCost{}
	for rows.Next() {
		var m model.ModelCost
		if err := rows.Scan(&m.Name, &m.Cost, &m.Calls, &m.InputTokens, &m.OutputTokens); err != nil {
			return nil, fmt.Errorf("clickhouse: scan model cost: %w", err)
		}
		m.Cost = sanitize(m.Cost)
		models = append(models, m)
	}
	return models, rows.Err()
}

// UserUsage returns the highest-spending users in the current window, each
// paired with its spend in the equal-length preceding window so the usage table
// can show per-column change. Current and previous are computed in a single pass
// over [PrevStart, End): the inner query rolls spans up to traces (cost, user,
// start time), and the outer query splits the two windows with sumIf/countIf at
// f.Start. Each row's cost-per-bucket Trend is filled separately over the
// current window.
func (r *ClickHouseRepository) UserUsage(ctx context.Context, f MetricsFilter, limit int) ([]model.UserUsage, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if f.UseRollup() {
		return r.userUsageRollup(ctx, f, limit)
	}
	cur := f.Start.UnixMilli()
	clause, args := window(f.UserID, f.WorkspaceIDs, f.PrevStart.UnixMilli(), f.End.UnixMilli())
	q := fmt.Sprintf(`
SELECT
    user_id,
    sumIf(cost, ts >= ?)          AS cost_cur,
    sumIf(cost, ts <  ?)          AS cost_prev,
    toInt64(countIf(ts >= ?))     AS runs_cur,
    toInt64(countIf(ts <  ?))     AS runs_prev
FROM (
    SELECT
        any(user_id)     AS user_id,
        sum(cost_usd)      AS cost,
        min(start_time_ms) AS ts
    FROM `+tableCalls+`
    WHERE %s
    GROUP BY trace_id
)
GROUP BY user_id
ORDER BY cost_cur DESC
LIMIT ?`, clause)

	qArgs := append([]any{cur, cur, cur, cur}, args...)
	qArgs = append(qArgs, limit)
	rows, err := r.db.QueryContext(ctx, q, qArgs...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: user usage: %w", err)
	}
	defer rows.Close()

	users := []model.UserUsage{}
	for rows.Next() {
		var t model.UserUsage
		if err := rows.Scan(&t.UserID, &t.Cost, &t.CostPrev, &t.Runs, &t.RunsPrev); err != nil {
			return nil, fmt.Errorf("clickhouse: scan user usage: %w", err)
		}
		t.Cost, t.CostPrev = sanitize(t.Cost), sanitize(t.CostPrev)
		users = append(users, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Each user's per-bucket cost over the current window, on the shared axis.
	bucketMs := f.Bucket.Milliseconds()
	trendClause, trendWArgs := window(f.UserID, f.WorkspaceIDs, f.Start.UnixMilli(), f.End.UnixMilli())
	trendQ := fmt.Sprintf(`
SELECT user_id, bucket_ms, sum(cost) AS cost
FROM (
    SELECT
        any(user_id)                    AS user_id,
        intDiv(min(start_time_ms), ?) * ? AS bucket_ms,
        sum(cost_usd)                     AS cost
    FROM `+tableCalls+`
    WHERE %s
    GROUP BY trace_id
)
GROUP BY user_id, bucket_ms`, trendClause)
	if err := r.fillUserTrends(ctx, f, users, trendQ, append([]any{bucketMs, bucketMs}, trendWArgs...)); err != nil {
		return nil, fmt.Errorf("clickhouse: user trends: %w", err)
	}
	return users, nil
}

// WorkflowUsage returns the highest-spending workflows in the current window paired
// with the preceding window (see UserUsage), plus each workflow's most-used
// model. Single pass over [PrevStart, End): the inner query derives a trace's
// workflow (see workflowExpr) and model and rolls up its cost; the outer query splits
// the windows with sumIf/countIf and takes the modal model with topK.
func (r *ClickHouseRepository) WorkflowUsage(ctx context.Context, f MetricsFilter, limit int) ([]model.WorkflowUsage, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if f.UseRollup() {
		return r.workflowUsageRollup(ctx, f, limit)
	}
	cur := f.Start.UnixMilli()
	clause, args := window(f.UserID, f.WorkspaceIDs, f.PrevStart.UnixMilli(), f.End.UnixMilli())
	q := fmt.Sprintf(`
SELECT
    name,
    topKIf(1)(model, model != '')[1] AS model,
    sumIf(cost, ts >= ?)             AS cost_cur,
    sumIf(cost, ts <  ?)             AS cost_prev,
    toInt64(countIf(ts >= ?))        AS runs_cur,
    toInt64(countIf(ts <  ?))        AS runs_prev
FROM (
    SELECT
        %s AS name,
        argMin(%s, start_time_ms) AS model,
        sum(cost_usd)      AS cost,
        min(start_time_ms) AS ts
    FROM `+tableCalls+`
    WHERE %s
    GROUP BY trace_id
)
GROUP BY name
ORDER BY cost_cur DESC
LIMIT ?`, workflowExpr, modelExpr, clause)

	qArgs := append([]any{cur, cur, cur, cur}, args...)
	qArgs = append(qArgs, limit)
	rows, err := r.db.QueryContext(ctx, q, qArgs...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: workflow usage: %w", err)
	}
	defer rows.Close()

	workflows := []model.WorkflowUsage{}
	for rows.Next() {
		var a model.WorkflowUsage
		if err := rows.Scan(&a.Name, &a.Model, &a.Cost, &a.CostPrev, &a.Runs, &a.RunsPrev); err != nil {
			return nil, fmt.Errorf("clickhouse: scan workflow usage: %w", err)
		}
		a.Cost, a.CostPrev = sanitize(a.Cost), sanitize(a.CostPrev)
		workflows = append(workflows, a)
	}
	return workflows, rows.Err()
}

// kpi pairs a current and previous value into a KPI. higherIsBad flips the
// delta classification for metrics where an increase is undesirable.
func kpi(cur, prev float64, higherIsBad bool) model.KPI {
	k := model.KPI{Value: cur, DeltaType: "neutral"}
	// No data in the previous period: there's no baseline to compare against, so
	// the move isn't good or bad — leave it neutral with a zero delta.
	if prev == 0 {
		return k
	}
	k.Delta = (cur - prev) / prev
	if cur > prev {
		k.DeltaType = boolToDelta(!higherIsBad)
	} else if cur < prev {
		k.DeltaType = boolToDelta(higherIsBad)
	}
	return k
}

func boolToDelta(good bool) string {
	if good {
		return "good"
	}
	return "bad"
}

// sanitize coerces NaN/Inf (e.g. a quantile over an empty set) to 0.
func sanitize(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// AttributeKeys returns the distinct custom-attribute keys seen in the window.
// It scans raw spans with no rollup path, so callers must keep f off rollup ranges.
func (r *ClickHouseRepository) AttributeKeys(ctx context.Context, f MetricsFilter, limit int) ([]string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	clause, args := window(f.UserID, f.WorkspaceIDs, f.Start.UnixMilli(), f.End.UnixMilli())
	q := fmt.Sprintf(`
SELECT DISTINCT arrayJoin(mapKeys(attributes)) AS key
FROM `+tableCalls+`
WHERE %s
ORDER BY key
LIMIT ?`, clause)

	rows, err := r.db.QueryContext(ctx, q, append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: attribute keys: %w", err)
	}
	defer rows.Close()

	keys := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("clickhouse: scan attribute key: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// UsageByAttribute groups spend/usage by the value of one custom attribute key,
// excluding spans without it. Like AttributeKeys it reads raw spans only.
func (r *ClickHouseRepository) UsageByAttribute(ctx context.Context, f MetricsFilter, key string, limit int) ([]model.AttributeUsage, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	clause, args := window(f.UserID, f.WorkspaceIDs, f.Start.UnixMilli(), f.End.UnixMilli())
	q := fmt.Sprintf(`
SELECT
    attributes[?]               AS value,
    sum(cost_usd)               AS cost,
    toInt64(count())            AS calls,
    toInt64(uniq(trace_id))     AS runs,
    toInt64(sum(input_tokens))  AS input_tokens,
    toInt64(sum(output_tokens)) AS output_tokens
FROM `+tableCalls+`
WHERE %s AND attributes[?] != ''
GROUP BY value
ORDER BY cost DESC
LIMIT ?`, clause)

	qArgs := append([]any{key}, args...)
	qArgs = append(qArgs, key, limit)
	rows, err := r.db.QueryContext(ctx, q, qArgs...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: usage by attribute: %w", err)
	}
	defer rows.Close()

	usage := []model.AttributeUsage{}
	for rows.Next() {
		var u model.AttributeUsage
		if err := rows.Scan(&u.Value, &u.Cost, &u.Calls, &u.Runs, &u.InputTokens, &u.OutputTokens); err != nil {
			return nil, fmt.Errorf("clickhouse: scan attribute usage: %w", err)
		}
		u.Cost = sanitize(u.Cost)
		usage = append(usage, u)
	}
	return usage, rows.Err()
}
