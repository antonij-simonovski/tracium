package query

import (
	"context"
	"fmt"
	"time"
)

// signalSampleSQL picks a metric's example runs: the trace filter and the
// order that puts the worst run first.
var signalSampleSQL = map[string]struct{ having, order string }{
	"truncation_rate": {"countIf(silent_failure = 'truncated') > 0", "max(start_time_ms) DESC"},
	"refusal_rate":    {"countIf(silent_failure = 'refused') > 0", "max(start_time_ms) DESC"},
	"tool_error_rate": {"countIf(silent_failure = 'tool_error') > 0", "max(start_time_ms) DESC"},
	"error_rate":      {"countIf" + spanErrored + " > 0", "max(start_time_ms) DESC"},
	"cost":            {"sum(cost_usd) > 0", "sum(cost_usd) DESC"},
	"latency_p95":     {"countIf(parent_span_id = '') > 0", "maxIf(end_time_ms - start_time_ms, parent_span_id = '') DESC"},
	"cache_hit_ratio": {"sumIf(input_tokens, kind = 'llm') > 0", "sumIf(input_tokens - cache_read_tokens, kind = 'llm') DESC"},
}

// SignalSamples returns up to limit runs that show metric's anomaly in the bucket
// at bucketMs, narrowed to f.Workflow when set. Metrics without samples return nil.
func (r *ClickHouseRepository) SignalSamples(ctx context.Context, f MetricsFilter, metric string, bucketMs int64, limit int) ([]string, error) {
	q, ok := signalSampleSQL[metric]
	if !ok {
		return nil, nil
	}
	width := time.Hour
	if metric == "cost" || metric == "error_rate" || metric == "latency_p95" {
		width = 24 * time.Hour
	}
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	clause, args := window(f.UserID, f.WorkspaceIDs, bucketMs, bucketMs+width.Milliseconds())
	if f.Workflow != "" {
		clause += " AND workflow_name = ?"
		args = append(args, f.Workflow)
	}
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`SELECT trace_id FROM tracium.spans WHERE source = 'span' AND %s
GROUP BY trace_id HAVING %s ORDER BY %s, trace_id LIMIT ?`, clause, q.having, q.order), append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: signal samples: %w", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("clickhouse: scan signal sample: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ActiveWorkspaces lists the workspaces with spans on or after since's day, from the daily rollup.
func (r *ClickHouseRepository) ActiveWorkspaces(ctx context.Context, since time.Time) ([]string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT workspace_id FROM tracium.metrics_daily WHERE bucket_date >= toDate(?)`, since.UTC().Format(time.DateOnly))
	if err != nil {
		return nil, fmt.Errorf("clickhouse: active workspaces: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("clickhouse: scan active workspace: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
