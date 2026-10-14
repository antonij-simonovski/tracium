package query

import (
	"context"
	"fmt"
	"time"

	"github.com/tracium/api/internal/model"
)

// This file serves the metrics queries from the daily rollup
// (tracium.metrics_daily, see the collector's 002/003 migrations) instead of raw
// spans. The rollup holds one row per (day, user, workflow, model), so a long
// window reads ~(days × cardinality) rows rather than every span in the window —
// the read cost tracks cardinality, not span volume.
//
// Each MetricsRepository method dispatches here when f.UseRollup() is true (long
// windows). Short windows stay on the raw-span path in metrics.go, which is exact
// and already O(window) fast.
//
// What the rollup can and cannot serve:
//   - cost, tokens, span counts: exact (additive sums).
//   - runs / error runs: distinct trace counts via uniq/uniqIf states, ~1%
//     approximate (HyperLogLog) — fine for dashboards and trend reporting.
//   - latency percentiles: NOT derivable (they need per-trace durations, which a
//     span→day rollup discards). LatencySeries returns a null-filled axis and the
//     LatencyP95 KPI is left neutral for long windows.
//   - workflow identity: the span's collector-derived workflow_name column, not the
//     per-trace argMin fallback used on the raw path; for the resource
//     service.name (the common case) these agree.
//   - workflow version: likewise the span's own, which matches versionExpr only
//     when every span carries the version (e.g. set as a resource attribute).

const dateFmt = "2006-01-02"

// rollupWindow builds the bucket_date range clause and args for the rollup. lo/hi
// bound the window; bucket_date is a Date, so they are passed as YYYY-MM-DD.
// hiInclusive includes the hi day (used for the current window so today's partial
// day is counted); the preceding window passes false so the windows never share a
// boundary day.
func rollupWindow(user string, workspaces []string, lo, hi time.Time, hiInclusive bool) (string, []any) {
	op := "<"
	if hiInclusive {
		op = "<="
	}
	clause := "bucket_date >= ? AND bucket_date " + op + " ?"
	args := []any{lo.Format(dateFmt), hi.Format(dateFmt)}
	if user != "" {
		clause += " AND user_id = ?"
		args = append(args, user)
	}
	wsClause, wsArgs := workspaceScope(workspaces)
	clause += wsClause
	args = append(args, wsArgs...)
	return clause, args
}

// bucketMsExpr converts a Date column to the epoch-ms of its UTC midnight, matching
// the grid bucketAxis builds from the window start (both are UTC day-aligned), so
// rollup buckets line up with the zero-fill axis. toInt64(Date) is days-since-epoch.
const bucketMsExpr = "toInt64(bucket_date) * 86400000"

// rollupKPI holds one window's rollup aggregates before they are paired into deltas.
type rollupKPI struct {
	cost          float64
	runs, errRuns int64
}

func rollupErrRate(k rollupKPI) float64 {
	if k.runs == 0 {
		return 0
	}
	return float64(k.errRuns) / float64(k.runs)
}

// costReconcileRollupExpr reconciles the daily cost rollup's two source columns
// within a bucket_date group, the day-grain analog of reconciledCostUnion.
const costReconcileRollupExpr = `greatest(sum(span_cost), sum(metric_cost))`

// costTotalRollupSQL sums per-day reconciled cost over the window: reconcile each
// bucket_date, then sum the days. Reads the source-aware cost rollup
// (metrics_daily_cost) so metric-derived spend is included.
func costTotalRollupSQL(clause string) string {
	return fmt.Sprintf(`SELECT sum(day_cost) FROM (
    SELECT %s AS day_cost
    FROM tracium.metrics_daily_cost WHERE %s GROUP BY bucket_date
)`, costReconcileRollupExpr, clause)
}

// costSeriesRollupSQL is the per-day reconciled cost series on the day grid the
// long-window chart uses, so the chart and the KPI agree.
func costSeriesRollupSQL(clause string) string {
	return fmt.Sprintf(`SELECT %s AS bucket_ms, %s AS value
FROM tracium.metrics_daily_cost WHERE %s GROUP BY bucket_ms`, bucketMsExpr, costReconcileRollupExpr, clause)
}

// costTotalRollup sums per-day reconciled cost over the window (see
// costTotalRollupSQL). Reads metrics_daily_cost, not metrics_daily, so
// metric-derived spend is included; with no metrics ingested it equals the
// span-only sum.
func (r *ClickHouseRepository) costTotalRollup(ctx context.Context, user string, workspaces []string, lo, hi time.Time, hiInclusive bool) (float64, error) {
	clause, args := rollupWindow(user, workspaces, lo, hi, hiInclusive)
	var cost float64
	if err := r.db.QueryRowContext(ctx, costTotalRollupSQL(clause), args...).Scan(&cost); err != nil {
		return 0, fmt.Errorf("clickhouse: cost total rollup: %w", err)
	}
	return sanitize(cost), nil
}

func (r *ClickHouseRepository) kpiWindowRollup(ctx context.Context, user string, workspaces []string, lo, hi time.Time, hiInclusive bool) (rollupKPI, error) {
	clause, args := rollupWindow(user, workspaces, lo, hi, hiInclusive)
	var k rollupKPI
	// runs/error_runs stay span-derived (metrics_daily); cost is reconciled
	// across both sources separately (costTotalRollup), mirroring how the raw
	// path splits costTotal out of kpiAggregates.
	q := fmt.Sprintf(`SELECT toInt64(uniqMerge(runs)), toInt64(uniqIfMerge(error_runs))
FROM tracium.metrics_daily WHERE %s`, clause)
	if err := r.db.QueryRowContext(ctx, q, args...).Scan(&k.runs, &k.errRuns); err != nil {
		return rollupKPI{}, fmt.Errorf("clickhouse: kpi rollup: %w", err)
	}

	cost, err := r.costTotalRollup(ctx, user, workspaces, lo, hi, hiInclusive)
	if err != nil {
		return rollupKPI{}, err
	}
	k.cost = cost
	return k, nil
}

func (r *ClickHouseRepository) overviewKPIsRollup(ctx context.Context, f MetricsFilter) (model.KPISet, error) {
	cur, err := r.kpiWindowRollup(ctx, f.UserID, f.WorkspaceIDs, f.Start, f.End, true)
	if err != nil {
		return model.KPISet{}, err
	}
	prev, err := r.kpiWindowRollup(ctx, f.UserID, f.WorkspaceIDs, f.PrevStart, f.Start, false)
	if err != nil {
		return model.KPISet{}, err
	}
	return model.KPISet{
		Cost: kpi(cur.cost, prev.cost, true),
		Runs: kpi(float64(cur.runs), float64(prev.runs), false),
		// Trace-duration p95 is not derivable from a span→day rollup; leave it
		// neutral. The dashboard hides the latency card for long windows.
		LatencyP95: model.KPI{DeltaType: "neutral"},
		ErrorRate:  kpi(rollupErrRate(cur), rollupErrRate(prev), true),
	}, nil
}

func (r *ClickHouseRepository) costSeriesRollup(ctx context.Context, f MetricsFilter) ([]model.CostPoint, error) {
	clause, args := rollupWindow(f.UserID, f.WorkspaceIDs, f.Start, f.End, true)
	// Reconcile the two cost sources per day (see costReconcileRollupExpr) on the
	// same day grid the long-window chart uses, so the chart and the KPI agree
	// and metric-derived spend is included.
	points, err := bucketSeries(ctx, r.db, f, costSeriesRollupSQL(clause), args, scanCostBucket, costPoint)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: cost series rollup: %w", err)
	}
	return points, nil
}

// latencySeriesEmpty returns the null-filled latency axis without querying:
// trace-duration percentiles can't come from the rollup, so long windows draw a
// gap. The axis still spans the window so it lines up with the cost/error charts.
func latencySeriesEmpty(f MetricsFilter) []model.LatencyPoint {
	base, bucketMs, n := bucketAxis(f)
	points := make([]model.LatencyPoint, n)
	for i := 0; i < n; i++ {
		points[i] = model.LatencyPoint{BucketMs: base + int64(i)*bucketMs}
	}
	return points
}

func (r *ClickHouseRepository) errorSeriesRollup(ctx context.Context, f MetricsFilter) ([]model.ErrorPoint, error) {
	clause, args := rollupWindow(f.UserID, f.WorkspaceIDs, f.Start, f.End, true)
	q := fmt.Sprintf(`SELECT %s AS bucket_ms,
    toInt64(uniqIfMerge(error_runs)) AS errors,
    toInt64(uniqMerge(runs))         AS total
FROM tracium.metrics_daily WHERE %s GROUP BY bucket_ms`, bucketMsExpr, clause)

	points, err := bucketSeries(ctx, r.db, f, q, args, scanErrorBucket, errorPoint)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: error series rollup: %w", err)
	}
	return points, nil
}

func tokenSeriesRollupSQL(clause string) string {
	return fmt.Sprintf(`SELECT %s AS bucket_ms,
    toInt64(sum(input_tokens))  AS input_tokens,
    toInt64(sum(output_tokens)) AS output_tokens
FROM tracium.metrics_daily WHERE %s GROUP BY bucket_ms`, bucketMsExpr, clause)
}

func (r *ClickHouseRepository) tokenSeriesRollup(ctx context.Context, f MetricsFilter) ([]model.TokenPoint, error) {
	clause, args := rollupWindow(f.UserID, f.WorkspaceIDs, f.Start, f.End, true)
	points, err := bucketSeries(ctx, r.db, f, tokenSeriesRollupSQL(clause), args, scanTokenBucket, tokenPoint)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: token series rollup: %w", err)
	}
	return points, nil
}

func (r *ClickHouseRepository) topWorkflowsRollup(ctx context.Context, f MetricsFilter, limit int) ([]model.WorkflowCost, error) {
	clause, args := rollupWindow(f.UserID, f.WorkspaceIDs, f.Start, f.End, true)
	q := fmt.Sprintf(`SELECT workflow_name AS name, sum(cost) AS cost, toInt64(uniqMerge(runs)) AS calls
FROM tracium.metrics_daily WHERE %s GROUP BY name ORDER BY cost DESC LIMIT ?`, clause)

	rows, err := r.db.QueryContext(ctx, q, append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: top workflows rollup: %w", err)
	}
	defer rows.Close()

	workflows := []model.WorkflowCost{}
	for rows.Next() {
		var a model.WorkflowCost
		if err := rows.Scan(&a.Name, &a.Cost, &a.Calls); err != nil {
			return nil, fmt.Errorf("clickhouse: scan workflow: %w", err)
		}
		a.Cost = sanitize(a.Cost)
		workflows = append(workflows, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	trendQ := fmt.Sprintf(`SELECT workflow_name AS name, %s AS bucket_ms, toInt64(uniqMerge(runs)) AS calls
FROM tracium.metrics_daily WHERE %s GROUP BY name, bucket_ms`, bucketMsExpr, clause)
	if err := fillWorkflowTrends(ctx, r.db, f, workflows, workflowCostTrend, trendQ, args); err != nil {
		return nil, fmt.Errorf("clickhouse: workflow trends rollup: %w", err)
	}
	return workflows, nil
}

func (r *ClickHouseRepository) listWorkflowsRollup(ctx context.Context, f MetricsFilter, limit int) ([]model.Workflow, error) {
	clause, args := rollupWindow(f.UserID, f.WorkspaceIDs, f.Start, f.End, true)
	// calls/error runs are uniq-approximate; avg latency is not derivable from a
	// span→day rollup (per-trace durations are discarded), so it is left 0 for
	// long windows — same limitation as LatencyP95. The dashboard shows "—".
	q := fmt.Sprintf(`SELECT workflow_name AS name,
    toInt64(uniqMerge(runs))         AS calls,
    sum(cost)                        AS cost,
    toInt64(uniqIfMerge(error_runs)) AS err_runs
FROM tracium.metrics_daily WHERE %s GROUP BY name ORDER BY calls DESC LIMIT ?`, clause)

	rows, err := r.db.QueryContext(ctx, q, append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: list workflows rollup: %w", err)
	}
	defer rows.Close()

	workflows := []model.Workflow{}
	for rows.Next() {
		var a model.Workflow
		var errRuns int64
		if err := rows.Scan(&a.Name, &a.Calls, &a.Cost, &errRuns); err != nil {
			return nil, fmt.Errorf("clickhouse: scan workflow: %w", err)
		}
		a.Cost = sanitize(a.Cost)
		if a.Calls > 0 {
			a.ErrorRate = float64(errRuns) / float64(a.Calls)
		}
		workflows = append(workflows, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	trendQ := fmt.Sprintf(`SELECT workflow_name AS name, %s AS bucket_ms, toInt64(uniqMerge(runs)) AS calls
FROM tracium.metrics_daily WHERE %s GROUP BY name, bucket_ms`, bucketMsExpr, clause)
	if err := fillWorkflowTrends(ctx, r.db, f, workflows, workflowTrend, trendQ, args); err != nil {
		return nil, fmt.Errorf("clickhouse: workflow trends rollup: %w", err)
	}
	return workflows, nil
}

func (r *ClickHouseRepository) failuresRollup(ctx context.Context, f MetricsFilter, limit int) ([]model.Failure, int64, error) {
	clause, args := rollupWindow(f.UserID, f.WorkspaceIDs, f.Start, f.End, true)
	// top_error is not stored in the rollup (no error_type dimension), so it is
	// left empty for long windows.
	q := fmt.Sprintf(`SELECT workflow_name AS workflow,
    toInt64(uniqIfMerge(error_runs)) AS failed,
    toInt64(uniqMerge(runs))         AS runs
FROM tracium.metrics_daily WHERE %s GROUP BY workflow HAVING failed > 0 ORDER BY failed DESC LIMIT ?`, clause)

	rows, err := r.db.QueryContext(ctx, q, append(args, limit)...)
	if err != nil {
		return nil, 0, fmt.Errorf("clickhouse: failures rollup: %w", err)
	}
	defer rows.Close()

	failures := []model.Failure{}
	for rows.Next() {
		var fl model.Failure
		var runs int64
		if err := rows.Scan(&fl.Workflow, &fl.Count, &runs); err != nil {
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

	var total int64
	totalQ := fmt.Sprintf(`SELECT toInt64(uniqIfMerge(error_runs)) FROM tracium.metrics_daily WHERE %s`, clause)
	if err := r.db.QueryRowContext(ctx, totalQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("clickhouse: failures total rollup: %w", err)
	}
	return failures, total, nil
}

func (r *ClickHouseRepository) modelCostsRollup(ctx context.Context, f MetricsFilter, limit int) ([]model.ModelCost, error) {
	clause, args := rollupWindow(f.UserID, f.WorkspaceIDs, f.Start, f.End, true)
	// Calls is span-level (sum of span_count), matching the raw model-costs path.
	q := fmt.Sprintf(`SELECT model AS name, sum(cost) AS cost,
    toInt64(sum(span_count))    AS calls,
    toInt64(sum(input_tokens))  AS input_tokens,
    toInt64(sum(output_tokens)) AS output_tokens
FROM tracium.metrics_daily WHERE %s AND model != '' GROUP BY name ORDER BY cost DESC LIMIT ?`, clause)

	rows, err := r.db.QueryContext(ctx, q, append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: model costs rollup: %w", err)
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

func (r *ClickHouseRepository) userUsageRollup(ctx context.Context, f MetricsFilter, limit int) ([]model.UserUsage, error) {
	// One pass over [PrevStart, End], split at the current window's start day.
	clause, args := rollupWindow(f.UserID, f.WorkspaceIDs, f.PrevStart, f.End, true)
	curDate := f.Start.Format(dateFmt)
	q := fmt.Sprintf(`SELECT user_id,
    sumIf(cost, bucket_date >= ?)                  AS cost_cur,
    sumIf(cost, bucket_date <  ?)                  AS cost_prev,
    toInt64(uniqMergeIf(runs, bucket_date >= ?))   AS runs_cur,
    toInt64(uniqMergeIf(runs, bucket_date <  ?))   AS runs_prev
FROM tracium.metrics_daily WHERE %s GROUP BY user_id ORDER BY cost_cur DESC LIMIT ?`, clause)

	qArgs := append([]any{curDate, curDate, curDate, curDate}, args...)
	qArgs = append(qArgs, limit)
	rows, err := r.db.QueryContext(ctx, q, qArgs...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: user usage rollup: %w", err)
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

	// Trends span the current window only (the main query spans [PrevStart, End]).
	trendClause, trendArgs := rollupWindow(f.UserID, f.WorkspaceIDs, f.Start, f.End, true)
	trendQ := fmt.Sprintf(`SELECT user_id, %s AS bucket_ms, sum(cost) AS cost
FROM tracium.metrics_daily WHERE %s GROUP BY user_id, bucket_ms`, bucketMsExpr, trendClause)
	if err := r.fillUserTrends(ctx, f, users, trendQ, trendArgs); err != nil {
		return nil, fmt.Errorf("clickhouse: user trends rollup: %w", err)
	}
	return users, nil
}

func (r *ClickHouseRepository) workflowUsageRollup(ctx context.Context, f MetricsFilter, limit int) ([]model.WorkflowUsage, error) {
	// Workflow-level cost/runs split (one pass over [PrevStart, End], split at Start).
	clause, args := rollupWindow(f.UserID, f.WorkspaceIDs, f.PrevStart, f.End, true)
	curDate := f.Start.Format(dateFmt)
	q := fmt.Sprintf(`SELECT workflow_name AS name,
    sumIf(cost, bucket_date >= ?)                AS cost_cur,
    sumIf(cost, bucket_date <  ?)                AS cost_prev,
    toInt64(uniqMergeIf(runs, bucket_date >= ?)) AS runs_cur,
    toInt64(uniqMergeIf(runs, bucket_date <  ?)) AS runs_prev
FROM tracium.metrics_daily WHERE %s GROUP BY name ORDER BY cost_cur DESC LIMIT ?`, clause)

	qArgs := append([]any{curDate, curDate, curDate, curDate}, args...)
	qArgs = append(qArgs, limit)
	rows, err := r.db.QueryContext(ctx, q, qArgs...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: workflow usage rollup: %w", err)
	}
	defer rows.Close()

	workflows := []model.WorkflowUsage{}
	for rows.Next() {
		var a model.WorkflowUsage
		if err := rows.Scan(&a.Name, &a.Cost, &a.CostPrev, &a.Runs, &a.RunsPrev); err != nil {
			return nil, fmt.Errorf("clickhouse: scan workflow usage: %w", err)
		}
		a.Cost, a.CostPrev = sanitize(a.Cost), sanitize(a.CostPrev)
		workflows = append(workflows, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.fillWorkflowModels(ctx, f, workflows); err != nil {
		return nil, err
	}
	return workflows, nil
}

// fillWorkflowModels sets each workflow's most-used model: the model with the most
// spans for that workflow over the current window. Runs can't be summed across
// models (a trace spanning two models would be counted twice), so the modal
// model is a separate span-count aggregation joined in Go by workflow name.
func (r *ClickHouseRepository) fillWorkflowModels(ctx context.Context, f MetricsFilter, workflows []model.WorkflowUsage) error {
	if len(workflows) == 0 {
		return nil
	}
	clause, args := rollupWindow(f.UserID, f.WorkspaceIDs, f.Start, f.End, true)
	q := fmt.Sprintf(`SELECT name, argMax(model, spans) AS model FROM (
    SELECT workflow_name AS name, model, sum(span_count) AS spans
    FROM tracium.metrics_daily WHERE %s AND model != '' GROUP BY name, model
) GROUP BY name`, clause)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("clickhouse: workflow models rollup: %w", err)
	}
	defer rows.Close()

	idx := make(map[string]int, len(workflows))
	for i := range workflows {
		idx[workflows[i].Name] = i
	}
	for rows.Next() {
		var name, modelName string
		if err := rows.Scan(&name, &modelName); err != nil {
			return fmt.Errorf("clickhouse: scan workflow model: %w", err)
		}
		if i, ok := idx[name]; ok {
			workflows[i].Model = modelName
		}
	}
	return rows.Err()
}
