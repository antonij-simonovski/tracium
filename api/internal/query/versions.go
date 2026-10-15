package query

import (
	"context"
	"fmt"

	"github.com/tracium/api/internal/model"
)

// WorkflowVersions lists f.Workflow's versions in the window, most recently seen first.
func (r *ClickHouseRepository) WorkflowVersions(ctx context.Context, f MetricsFilter, limit int) ([]model.WorkflowVersion, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if f.UseRollup() {
		return r.workflowVersionsRollup(ctx, f, limit)
	}
	clause, args := window(f.UserID, f.WorkspaceIDs, f.Start.UnixMilli(), f.End.UnixMilli())
	q := fmt.Sprintf(`
SELECT
    version,
    min(ts),
    max(ts),
    toInt64(count()),
    sum(cost),
    quantile(0.95)(dur),
    toInt64(countIf(errored)),
    toInt64(sum(tokens))
FROM (
    SELECT
        %s AS name,
        %s AS version,
        min(start_time_ms)   AS ts,
        sum(cost_usd)        AS cost,
        `+traceDurationExpr+` AS dur,
        max(`+spanErrored+`) AS errored,
        sum(input_tokens + output_tokens) AS tokens
    FROM `+tableCalls+`
    WHERE %s
    GROUP BY trace_id
)
WHERE name = ?
GROUP BY version
ORDER BY max(ts) DESC
LIMIT ?`, workflowExpr, versionExpr, clause)

	return r.scanVersions(ctx, q, append(args, f.Workflow, limit), true)
}

// workflowVersionsRollup serves long windows; it has no latency (see the rollup.go caveats).
func (r *ClickHouseRepository) workflowVersionsRollup(ctx context.Context, f MetricsFilter, limit int) ([]model.WorkflowVersion, error) {
	clause, args := rollupWindow(f.UserID, f.WorkspaceIDs, f.Start, f.End, true)
	q := fmt.Sprintf(`SELECT workflow_version,
    min(%[1]s),
    max(%[1]s),
    toInt64(uniqMerge(runs)),
    sum(cost),
    toFloat64(0),
    toInt64(uniqIfMerge(error_runs)),
    toInt64(sum(input_tokens) + sum(output_tokens))
FROM tracium.metrics_daily WHERE %[2]s AND workflow_name = ?
GROUP BY workflow_version ORDER BY max(bucket_date) DESC LIMIT ?`, bucketMsExpr, clause)

	return r.scanVersions(ctx, q, append(args, f.Workflow, limit), false)
}

func (r *ClickHouseRepository) scanVersions(ctx context.Context, q string, args []any, withLatency bool) ([]model.WorkflowVersion, error) {
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: workflow versions: %w", err)
	}
	defer rows.Close()

	versions := []model.WorkflowVersion{}
	for rows.Next() {
		var (
			v               model.WorkflowVersion
			p95             float64
			errRuns, tokens int64
		)
		if err := rows.Scan(&v.Version, &v.FirstSeenMs, &v.LastSeenMs,
			&v.Calls, &v.Cost, &p95, &errRuns, &tokens); err != nil {
			return nil, fmt.Errorf("clickhouse: scan workflow version: %w", err)
		}
		v.Cost = sanitize(v.Cost)
		if withLatency {
			if s := sanitize(p95); s > 0 {
				v.P95LatencyMs = &s
			}
		}
		if v.Calls > 0 {
			v.CostPerRun = v.Cost / float64(v.Calls)
			v.ErrorRate = float64(errRuns) / float64(v.Calls)
			v.TokensPerRun = float64(tokens) / float64(v.Calls)
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}
