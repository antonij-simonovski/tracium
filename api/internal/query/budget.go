package query

import (
	"context"
	"fmt"
	"time"
)

// spendSQL sums cost since a day by scope's subject. Workflow cost is span
// cost from metrics_daily, as on the scoped cost chart: metric rows carry no workflow.
func spendSQL(scope, clause string) (string, error) {
	switch scope {
	case "workspace":
		return costTotalRollupSQL(clause), nil
	case "client":
		return fmt.Sprintf(`SELECT user_id, sum(day_cost) FROM (
    SELECT user_id, %s AS day_cost FROM tracium.metrics_daily_cost WHERE %s GROUP BY bucket_date, user_id
) GROUP BY user_id`, costReconcileExpr, clause), nil
	case "workflow":
		return fmt.Sprintf(`SELECT workflow_name, sum(cost) FROM tracium.metrics_daily WHERE %s GROUP BY workflow_name`, clause), nil
	}
	return "", fmt.Errorf("spend: unknown scope %q", scope)
}

// Spend is one workspace's cost from since's UTC day through today, by
// workflow or client, or under "" for scope workspace. Reads the daily rollups only.
func (r *ClickHouseRepository) Spend(ctx context.Context, workspaceID, scope string, since time.Time) (map[string]float64, error) {
	clause, args := rollupWindow("", []string{workspaceID}, since.UTC(), time.Now().UTC(), true)
	q, err := spendSQL(scope, clause)
	if err != nil {
		return nil, err
	}
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	out := map[string]float64{}
	if scope == "workspace" {
		var cost float64
		if err := r.db.QueryRowContext(ctx, q, args...).Scan(&cost); err != nil {
			return nil, fmt.Errorf("clickhouse: spend: %w", err)
		}
		out[""] = sanitize(cost)
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: spend: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var subject string
		var cost float64
		if err := rows.Scan(&subject, &cost); err != nil {
			return nil, fmt.Errorf("clickhouse: scan spend: %w", err)
		}
		out[subject] = sanitize(cost)
	}
	return out, rows.Err()
}
