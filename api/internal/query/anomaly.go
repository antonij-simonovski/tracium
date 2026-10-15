package query

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/tracium/api/internal/anomaly"
	"github.com/tracium/api/internal/model"
)

// Anomaly detection is daily and served entirely from the daily rollup
// (tracium.metrics_daily), regardless of the display range: the baseline reaches
// ~4 weeks before the display window, and the rollup holds all of that history
// cheaply. Because the rollup's size tracks cardinality (days × dimensions), not
// span volume, the read cost is ~(baseline+window days) × (top-N workflows) rows —
// a few thousand — no matter how many spans were ingested. This is why anomaly
// detection stays fast at high trace volume where a raw-span scan would not.
//
// It does NOT dispatch on f.UseRollup(): even a 7d display reads the rollup here,
// because the baseline needs history that predates the window.
const (
	// anomalyBaselineDays is how many days precede a candidate as its baseline.
	// Four weeks: enough robust samples to be stable, short enough to track real
	// drift in usage. (Weekly seasonality is not modelled in v1 — see the package
	// doc in internal/anomaly.)
	anomalyBaselineDays = 28
	// anomalyMinBaseline is the fewest baseline days needed to score a bucket.
	anomalyMinBaseline = 7
	// anomalyMinBaselineActive is the fewest baseline days that must carry real
	// traffic before the baseline is trusted. This is the "enough traces to form a
	// baseline" rule: a workspace that has only sent data for a day or two (its
	// gap-filled baseline is almost all zeros) is not scored at all, rather than
	// having its first busy day flagged against a ~0 baseline.
	anomalyMinBaselineActive = 10
	// anomalyWorkflowLimit caps how many workflows are scored per request (the busiest
	// by run volume), keeping the per-workflow series query bounded.
	anomalyWorkflowLimit = 50
	// anomalyResultLimit caps the returned anomalies (most severe first).
	anomalyResultLimit = 100
)

// Shared robust-z thresholds for all metrics (|z| bands).
const (
	anomalyZInfo     = 3.0
	anomalyZWarning  = 4.5
	anomalyZCritical = 6.0
)

// anomalyMetric declares one detectable metric: how to project a daily aggregate
// onto a scored series, and the floors/directions that suit its units.
type anomalyMetric struct {
	name        string // model.Anomaly.Metric value
	absFloor    float64
	volumeFloor float64
	spike, drop bool
}

// anomalyMetrics is the v1 detection set: cost (spikes), error rate (spikes),
// and run volume (spikes and drops — a drop toward zero is the silent-failure
// signal). Latency and per-model/user slices are deferred (see the design).
var anomalyMetrics = []anomalyMetric{
	{name: "cost", absFloor: 1.0, volumeFloor: 1.0, spike: true},
	{name: "error_rate", absFloor: 0.05, volumeFloor: 20, spike: true},
	{name: "runs", absFloor: 5, volumeFloor: 10, spike: true, drop: true},
}

// dailyAgg is one bucket's rollup aggregates for one series (overall or one workflow).
type dailyAgg struct {
	cost    float64
	runs    int64
	errRuns int64
}

// Anomalies detects statistical anomalies over the window in f, workspace-scoped.
// It scores the workspace-wide series and each busy workflow's series for cost,
// error rate, and run volume, and returns the flagged buckets ordered most
// severe first. Optional f.AnomalyMetric / f.AnomalyMinSeverity narrow the output.
func (r *ClickHouseRepository) Anomalies(ctx context.Context, f MetricsFilter) ([]model.Anomaly, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	// Daily grid over [baseStart, End]: display window plus the baseline lead-in.
	daily := f
	daily.Bucket = 24 * time.Hour
	baseStart := f.Start.AddDate(0, 0, -anomalyBaselineDays)
	ext := daily
	ext.Start = baseStart
	base, bucketMs, n := bucketAxis(ext)
	axis := make([]int64, n)
	for i := 0; i < n; i++ {
		axis[i] = base + int64(i)*bucketMs
	}
	// Only buckets at/after the display window's first day are reportable; earlier
	// buckets are baseline-only.
	detectFrom, _, _ := bucketAxis(daily)

	minSev := severityRank(f.AnomalyMinSeverity)

	var out []model.Anomaly

	overall, err := r.anomalyDailySeries(ctx, f, baseStart)
	if err != nil {
		return nil, err
	}
	out = append(out, detectSeries(f, "workspace", "", axis, overall, detectFrom, minSev)...)

	workflows, err := r.anomalyTopWorkflows(ctx, f)
	if err != nil {
		return nil, err
	}
	if len(workflows) > 0 {
		byWorkflow, err := r.anomalyWorkflowSeries(ctx, f, baseStart, workflows)
		if err != nil {
			return nil, err
		}
		for _, name := range workflows {
			out = append(out, detectSeries(f, "workflow", name, axis, byWorkflow[name], detectFrom, minSev)...)
		}
	}

	if len(out) > 0 {
		firsts, err := r.versionFirstSeen(ctx, f, detectFrom-versionLeadMs)
		if err != nil {
			return nil, err
		}
		attachVersionChanges(out, firsts)
	}

	sortAnomalies(out)
	if len(out) > anomalyResultLimit {
		out = out[:anomalyResultLimit]
	}
	return out, nil
}

// detectSeries runs every enabled metric's detector over one series (zero-filled
// onto axis) and returns the resulting model.Anomaly rows. metric/severity
// filters from f are applied here so suppressed work is skipped cheaply.
func detectSeries(f MetricsFilter, scope, workflow string, axis []int64, byBucket map[int64]dailyAgg, detectFrom int64, minSev int) []model.Anomaly {
	var out []model.Anomaly
	for _, m := range anomalyMetrics {
		if f.AnomalyMetric != "" && f.AnomalyMetric != m.name {
			continue
		}
		pts := seriesPoints(axis, byBucket, m.name)
		opts := anomaly.Options{
			BaselineDays:      anomalyBaselineDays,
			MinBaseline:       anomalyMinBaseline,
			MinBaselineActive: anomalyMinBaselineActive,
			DetectFromMs:      detectFrom,
			ZInfo:             anomalyZInfo,
			ZWarning:          anomalyZWarning,
			ZCritical:         anomalyZCritical,
			AbsFloor:          m.absFloor,
			VolumeFloor:       m.volumeFloor,
			Spike:             m.spike,
			Drop:              m.drop,
		}
		for _, res := range anomaly.Detect(pts, opts) {
			if severityRank(string(res.Severity)) < minSev {
				continue
			}
			a := model.Anomaly{
				Metric:    m.name,
				Scope:     scope,
				Workflow:  workflow,
				BucketMs:  res.BucketMs,
				Observed:  sanitize(res.Observed),
				Expected:  sanitize(res.Expected),
				Deviation: sanitize(res.Deviation),
				Score:     sanitize(res.Score),
				Direction: string(res.Direction),
				Severity:  string(res.Severity),
			}
			a.Summary = anomalySummary(a)
			out = append(out, a)
		}
	}
	return out
}

// seriesPoints projects a bucket→aggregate map onto the gap-free axis as scored
// points. Absent buckets read as zero (a genuine no-activity day), which is what
// makes a drop toward zero a real point rather than a missing row. For the ratio
// metric error_rate, Support carries the run count so the volume floor gates on
// runs, not on the rate.
func seriesPoints(axis []int64, byBucket map[int64]dailyAgg, metric string) []anomaly.Point {
	pts := make([]anomaly.Point, len(axis))
	for i, bm := range axis {
		d := byBucket[bm]
		var v, sup float64
		switch metric {
		case "cost":
			v, sup = d.cost, d.cost
		case "runs":
			v, sup = float64(d.runs), float64(d.runs)
		case "error_rate":
			sup = float64(d.runs)
			if d.runs > 0 {
				v = float64(d.errRuns) / float64(d.runs)
			}
		}
		pts[i] = anomaly.Point{BucketMs: bm, Value: sanitize(v), Support: sup}
	}
	return pts
}

// anomalyDailySeries loads the workspace-wide daily aggregates over [baseStart, End].
func (r *ClickHouseRepository) anomalyDailySeries(ctx context.Context, f MetricsFilter, baseStart time.Time) (map[int64]dailyAgg, error) {
	clause, args := rollupWindow(f.UserID, f.WorkspaceIDs, baseStart, f.End, true)
	q := fmt.Sprintf(`SELECT %s AS bucket_ms,
    sum(cost)                        AS cost,
    toInt64(uniqMerge(runs))         AS runs,
    toInt64(uniqIfMerge(error_runs)) AS err_runs
FROM tracium.metrics_daily WHERE %s GROUP BY bucket_ms`, bucketMsExpr, clause)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: anomaly series: %w", err)
	}
	defer rows.Close()

	byBucket := make(map[int64]dailyAgg)
	for rows.Next() {
		var bm int64
		var d dailyAgg
		if err := rows.Scan(&bm, &d.cost, &d.runs, &d.errRuns); err != nil {
			return nil, fmt.Errorf("clickhouse: scan anomaly bucket: %w", err)
		}
		byBucket[bm] = d
	}
	return byBucket, rows.Err()
}

// anomalyTopWorkflows returns the busiest named workflows in the display window by run
// volume, capped at anomalyWorkflowLimit — the set whose series are worth scoring.
func (r *ClickHouseRepository) anomalyTopWorkflows(ctx context.Context, f MetricsFilter) ([]string, error) {
	clause, args := rollupWindow(f.UserID, f.WorkspaceIDs, f.Start, f.End, true)
	q := fmt.Sprintf(`SELECT workflow_name FROM tracium.metrics_daily
WHERE %s AND workflow_name != '' GROUP BY workflow_name ORDER BY uniqMerge(runs) DESC LIMIT ?`, clause)

	rows, err := r.db.QueryContext(ctx, q, append(args, anomalyWorkflowLimit)...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: anomaly top workflows: %w", err)
	}
	defer rows.Close()

	var workflows []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("clickhouse: scan anomaly workflow: %w", err)
		}
		workflows = append(workflows, name)
	}
	return workflows, rows.Err()
}

// anomalyWorkflowSeries loads daily aggregates over [baseStart, End] for the given
// workflows, keyed by workflow name then bucket.
func (r *ClickHouseRepository) anomalyWorkflowSeries(ctx context.Context, f MetricsFilter, baseStart time.Time, workflows []string) (map[string]map[int64]dailyAgg, error) {
	clause, args := rollupWindow(f.UserID, f.WorkspaceIDs, baseStart, f.End, true)
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(workflows)), ",")
	inArgs := make([]any, len(workflows))
	for i, a := range workflows {
		inArgs[i] = a
	}
	q := fmt.Sprintf(`SELECT workflow_name, %s AS bucket_ms,
    sum(cost)                        AS cost,
    toInt64(uniqMerge(runs))         AS runs,
    toInt64(uniqIfMerge(error_runs)) AS err_runs
FROM tracium.metrics_daily WHERE %s AND workflow_name IN (%s)
GROUP BY workflow_name, bucket_ms`, bucketMsExpr, clause, placeholders)

	rows, err := r.db.QueryContext(ctx, q, append(args, inArgs...)...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: anomaly workflow series: %w", err)
	}
	defer rows.Close()

	byWorkflow := make(map[string]map[int64]dailyAgg, len(workflows))
	for rows.Next() {
		var name string
		var bm int64
		var d dailyAgg
		if err := rows.Scan(&name, &bm, &d.cost, &d.runs, &d.errRuns); err != nil {
			return nil, fmt.Errorf("clickhouse: scan anomaly workflow bucket: %w", err)
		}
		if byWorkflow[name] == nil {
			byWorkflow[name] = make(map[int64]dailyAgg)
		}
		byWorkflow[name][bm] = d
	}
	return byWorkflow, rows.Err()
}

// versionLeadMs is how far before an anomaly's day a first-seen version still counts as near it.
const versionLeadMs = 24 * 60 * 60 * 1000

// versionFirstSeen returns workflow versions first seen at or after since, earliest first.
func (r *ClickHouseRepository) versionFirstSeen(ctx context.Context, f MetricsFilter, since int64) ([]model.VersionChange, error) {
	clause, args := rollupWindow(f.UserID, f.WorkspaceIDs, time.Unix(0, 0).UTC(), f.End, true)
	q := fmt.Sprintf(`SELECT workflow_name, workflow_version, min(%s) AS first_seen
FROM tracium.metrics_daily WHERE %s AND workflow_name != '' AND workflow_version != ''
GROUP BY workflow_name, workflow_version HAVING first_seen >= ?
ORDER BY first_seen, workflow_name, workflow_version`, bucketMsExpr, clause)

	rows, err := r.db.QueryContext(ctx, q, append(args, since)...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: version first seen: %w", err)
	}
	defer rows.Close()

	var out []model.VersionChange
	for rows.Next() {
		var v model.VersionChange
		if err := rows.Scan(&v.Workflow, &v.Version, &v.FirstSeenMs); err != nil {
			return nil, fmt.Errorf("clickhouse: scan version first seen: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// attachVersionChanges gives each anomaly the latest version first seen within
// versionLeadMs before its bucket, of its own workflow for workflow-scoped ones.
func attachVersionChanges(anomalies []model.Anomaly, firsts []model.VersionChange) {
	for i := range anomalies {
		a := &anomalies[i]
		for j := range firsts {
			v := firsts[j]
			if v.FirstSeenMs > a.BucketMs {
				break
			}
			if v.FirstSeenMs < a.BucketMs-versionLeadMs || (a.Scope == "workflow" && v.Workflow != a.Workflow) {
				continue
			}
			a.VersionChange = &firsts[j]
		}
		if a.VersionChange != nil {
			a.Summary += fmt.Sprintf(" Started near %s %s (first seen %s).", a.VersionChange.Workflow, a.VersionChange.Version,
				time.UnixMilli(a.VersionChange.FirstSeenMs).UTC().Format(dateFmt))
		}
	}
}

// sortAnomalies orders anomalies most-actionable first: by severity, then by
// absolute score, then most recent bucket.
func sortAnomalies(a []model.Anomaly) {
	sort.SliceStable(a, func(i, j int) bool {
		si, sj := severityRank(a[i].Severity), severityRank(a[j].Severity)
		if si != sj {
			return si > sj
		}
		mi, mj := math.Abs(a[i].Score), math.Abs(a[j].Score)
		if mi != mj {
			return mi > mj
		}
		return a[i].BucketMs > a[j].BucketMs
	})
}

// severityRank maps a severity to an ordinal for comparison; "" (unset) is 0, so
// an empty AnomalyMinSeverity admits everything.
func severityRank(s string) int {
	switch s {
	case "critical":
		return 3
	case "warning":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

// anomalySummary renders a one-line human description from an anomaly's fields.
func anomalySummary(a model.Anomaly) string {
	subject := "Workspace"
	if a.Scope == "workflow" {
		subject = fmt.Sprintf("Workflow %q", a.Workflow)
	}
	date := time.UnixMilli(a.BucketMs).UTC().Format(dateFmt)
	noun := map[string]string{"cost": "cost", "runs": "run volume", "error_rate": "error rate"}[a.Metric]
	obs := formatAnomalyValue(a.Metric, a.Observed)
	exp := formatAnomalyValue(a.Metric, a.Expected)
	if a.Direction == "drop" {
		return fmt.Sprintf("%s %s fell to %s on %s, from a typical %s.", subject, noun, obs, date, exp)
	}
	if a.Expected > 0 {
		return fmt.Sprintf("%s %s rose to %s on %s, %.1f× the typical %s.", subject, noun, obs, date, a.Observed/a.Expected, exp)
	}
	return fmt.Sprintf("%s %s rose to %s on %s, from near zero.", subject, noun, obs, date)
}

func formatAnomalyValue(metric string, v float64) string {
	switch metric {
	case "cost":
		return fmt.Sprintf("$%.2f", v)
	case "error_rate":
		return fmt.Sprintf("%.1f%%", v*100)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}
