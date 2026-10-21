package query

import (
	"strings"
	"testing"
	"time"
)

// The long-window (rollup) cost path must read the source-aware cost rollup and
// reconcile the two ingestion sources per day, exactly as the raw-span path
// reconciles per bucket. A regression to metrics_daily's span-only sum(cost) is
// precisely the bug that made 90d/1y dashboards omit metric-derived spend, so
// these assertions guard the table, the reconciliation, and the day grain.
func TestRollupCostQueriesReconcileBothSources(t *testing.T) {
	const clause = "bucket_date >= ? AND bucket_date <= ?"

	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"total", costTotalRollupSQL(clause)},
		{"series", costSeriesRollupSQL(clause)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Reads the reconciled cost rollup, never the span-only metrics_daily.
			if !strings.Contains(tc.sql, "tracium.metrics_daily_cost") {
				t.Errorf("query does not read tracium.metrics_daily_cost:\n%s", tc.sql)
			}
			if strings.Contains(tc.sql, "tracium.metrics_daily ") || strings.Contains(tc.sql, "tracium.metrics_daily\n") {
				t.Errorf("query reads span-only metrics_daily:\n%s", tc.sql)
			}
			// Reconciles greatest(span, metric); a plain sum(cost) would be the bug.
			if !strings.Contains(tc.sql, costReconcileExpr) {
				t.Errorf("query does not reconcile both sources:\n%s", tc.sql)
			}
			if strings.Contains(tc.sql, "sum(cost)") {
				t.Errorf("query still sums the span-only cost column:\n%s", tc.sql)
			}
			// The caller's window clause is threaded through.
			if !strings.Contains(tc.sql, clause) {
				t.Errorf("query dropped the window clause:\n%s", tc.sql)
			}
		})
	}

	// The total sums per-day reconciled cost (reconcile each day, then sum days),
	// the day-grain analog of the raw path's per-bucket reconciliation.
	total := costTotalRollupSQL(clause)
	if !strings.Contains(total, "GROUP BY bucket_date") {
		t.Errorf("total does not reconcile per day before summing:\n%s", total)
	}

	// The series buckets on the same UTC-midnight grid bucketAxis builds.
	series := costSeriesRollupSQL(clause)
	if !strings.Contains(series, bucketMsExpr+" AS bucket_ms") {
		t.Errorf("series does not emit bucket_ms on the shared grid:\n%s", series)
	}
}

// The long-window token series reads the span rollup, where the token sums live,
// on the same day grid as the cost series.
func TestTokenSeriesRollupSQL(t *testing.T) {
	const clause = "bucket_date >= ? AND bucket_date <= ?"
	q := tokenSeriesRollupSQL(clause)
	for _, want := range []string{
		"FROM tracium.metrics_daily WHERE " + clause,
		bucketMsExpr + " AS bucket_ms",
		"sum(input_tokens)",
		"sum(output_tokens)",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query missing %q:\n%s", want, q)
		}
	}
}

// Workflow- and version-scoped long-window queries filter the span rollup; the
// metric cost relation has neither, so the scoped cost series is span-only.
func TestWorkflowRollupScope(t *testing.T) {
	f := MetricsFilter{Workflow: "planner", Version: "v12", Start: time.Now().Add(-90 * 24 * time.Hour), End: time.Now()}
	clause, args := rollupScope(f)
	n := len(args)
	if !strings.HasSuffix(clause, " AND workflow_name = ? AND workflow_version = ?") || args[n-2] != "planner" || args[n-1] != "v12" {
		t.Errorf("scope = %q %v, want workflow_name then workflow_version bound last", clause, args)
	}
	if clause, _ := rollupScope(MetricsFilter{}); strings.Contains(clause, "workflow_name") {
		t.Errorf("unscoped clause = %q, want no workflow filter", clause)
	}
	for _, q := range []string{scopedCostSeriesRollupSQL(clause), workflowDetailRollupSQL(clause)} {
		if !strings.Contains(q, "FROM tracium.metrics_daily WHERE "+clause) {
			t.Errorf("query does not read the span rollup:\n%s", q)
		}
	}
}
