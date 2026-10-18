package query

import (
	"fmt"
	"time"
)

// TraceFilter holds all optional filtering criteria for listing traces.
type TraceFilter struct {
	UserID       string    // optional: the operator's end-client (a business dimension, not an access boundary)
	WorkspaceIDs []string  // access scope: the workspaces the caller may read. Empty means "no accessible workspace" and matches nothing (see workspaceScope). Set by the handler from the caller's memberships.
	Model        string    // optional model filter
	Workflow     string    // optional: restrict to one derived workflow (see workflowExpr), e.g. a workflow's recent runs
	Version      string    // optional: restrict to one derived workflow version (see versionExpr)
	HasError     *bool     // pointer to distinguish false from unset
	StartAfter   time.Time // optional lower bound on trace start time
	StartBefore  time.Time // optional upper bound on trace start time
	Page         int       // 1-indexed, default 1
	PageSize     int       // default 50, max 200
}

// defaultTraceWindow bounds a listing that arrives without an explicit lower
// bound to the last 30 days. Every trace listing must be time-bounded: the spans
// table is ordered by start_time_ms (see 001_create_spans.sql), so a bounded
// query prunes to its window's granules instead of aggregating the whole table.
const defaultTraceWindow = 30 * 24 * time.Hour

// Validate applies defaults and enforces constraints on the filter.
// It must be called before the filter is passed to any repository method.
func (f *TraceFilter) Validate() error {
	if f.PageSize > 200 {
		return fmt.Errorf("page_size must be <= 200")
	}
	if f.PageSize <= 0 {
		f.PageSize = 50
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	// Never let a listing run unbounded: default the lower bound so the query
	// always has a time window to prune to.
	if f.StartAfter.IsZero() {
		f.StartAfter = time.Now().Add(-defaultTraceWindow)
	}
	return nil
}

// MetricsFilter is the resolved time window for an overview metrics query.
// Start/End bound the current window; PrevStart..Start is the equal-length
// preceding window used to compute period-over-period deltas.
type MetricsFilter struct {
	UserID       string        // optional business filter (the operator's end-client)
	WorkspaceIDs []string      // access scope: the workspaces the caller may read (empty matches nothing). Set by the handler from memberships.
	Workflow     string        // optional: restrict the metric to one derived workflow (see workflowExpr)
	Version      string        // optional: restrict the metric to one derived workflow version (see versionExpr)
	Start        time.Time     // window lower bound (inclusive)
	End          time.Time     // window upper bound (exclusive)
	PrevStart    time.Time     // preceding window lower bound
	Bucket       time.Duration // time-series bucket width

	// Anomaly-detection filters (optional; ignored by every non-anomaly query).
	// AnomalyMetric restricts detection to one metric ("cost" | "error_rate" |
	// "runs"); AnomalyMinSeverity drops anomalies below a severity ("info" |
	// "warning" | "critical"). Empty means no restriction.
	AnomalyMetric      string
	AnomalyMinSeverity string
}

// rollupThreshold: windows longer than this are served from the daily rollup
// (tracium.metrics_daily) instead of raw spans. Shorter windows stay on raw —
// it is exact and already O(window) fast — while long windows would otherwise
// scan a quarter/year of spans, so they read the pre-aggregated table instead.
const rollupThreshold = 30 * 24 * time.Hour

// UseRollup reports whether this window should be served from the daily rollup.
// The rollup is daily-bucketed, so it only applies to ranges bucketed by day or
// coarser; the threshold keeps 24h/7d/30d on raw spans and routes 90d/1y to the
// rollup.
func (f MetricsFilter) UseRollup() bool {
	return f.End.Sub(f.Start) > rollupThreshold
}

// ParseRange resolves a range token into a MetricsFilter anchored at now. 24h is
// bucketed hourly; 7d/30d/90d/1y are bucketed daily. Ranges longer than 30d are
// served from the daily rollup (see UseRollup).
func ParseRange(rng string, now time.Time) (MetricsFilter, error) {
	var span, bucket time.Duration
	switch rng {
	case "24h":
		span, bucket = 24*time.Hour, time.Hour
	case "7d":
		span, bucket = 7*24*time.Hour, 24*time.Hour
	case "30d":
		span, bucket = 30*24*time.Hour, 24*time.Hour
	case "90d":
		span, bucket = 90*24*time.Hour, 24*time.Hour
	case "1y":
		span, bucket = 365*24*time.Hour, 24*time.Hour
	default:
		return MetricsFilter{}, fmt.Errorf("range must be one of 24h, 7d, 30d, 90d, 1y")
	}
	start := now.Add(-span)
	return MetricsFilter{
		Start:     start,
		End:       now,
		PrevStart: start.Add(-span),
		Bucket:    bucket,
	}, nil
}
