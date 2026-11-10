package query

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

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

const (
	newVersionHistory = 28 * 24 * time.Hour
	// maxNewVersions in a day marks a workflow that mints a version per run (see version_per_run).
	maxNewVersions = 5
)

type versionHour struct {
	workflow, version string
	hourMs, firstMs   int64
	runs              int64
}

// NewVersions lists the workflow versions first seen since `since` and not in
// the 28 days before it, each with the version it replaced. A workflow with no
// earlier version, or one minting more than five versions a day, is left out.
func (r *ClickHouseRepository) NewVersions(ctx context.Context, f MetricsFilter, since time.Time) ([]model.NewVersion, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	clause, args := window("", f.WorkspaceIDs, since.Add(-24*time.Hour).UnixMilli(), f.End.UnixMilli())
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
SELECT name, version, intDiv(ts, 3600000) * 3600000 AS hour, min(ts), toInt64(count())
FROM (
    SELECT %s AS name, %s AS version, min(start_time_ms) AS ts
    FROM `+tableCalls+`
    WHERE %s
    GROUP BY trace_id
)
WHERE version != ''
GROUP BY name, version, hour`, workflowExpr, versionExpr, clause), args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: new versions: %w", err)
	}
	defer rows.Close()
	var hours []versionHour
	for rows.Next() {
		var h versionHour
		if err := rows.Scan(&h.workflow, &h.version, &h.hourMs, &h.firstMs, &h.runs); err != nil {
			return nil, fmt.Errorf("clickhouse: scan new version: %w", err)
		}
		hours = append(hours, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	hclause, hargs := rollupWindow("", f.WorkspaceIDs, since.Add(-newVersionHistory), since, false)
	hrows, err := r.db.QueryContext(ctx, `SELECT DISTINCT workflow_name, workflow_version FROM tracium.metrics_daily
WHERE `+hclause+` AND workflow_version != ''`, hargs...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: version history: %w", err)
	}
	defer hrows.Close()
	seen := map[[2]string]bool{}
	for hrows.Next() {
		var k [2]string
		if err := hrows.Scan(&k[0], &k[1]); err != nil {
			return nil, fmt.Errorf("clickhouse: scan version history: %w", err)
		}
		seen[k] = true
	}
	if err := hrows.Err(); err != nil {
		return nil, err
	}
	return pickNewVersions(hours, seen, since, f.End), nil
}

func pickNewVersions(hours []versionHour, seen map[[2]string]bool, since, end time.Time) []model.NewVersion {
	type pair = [2]string
	first, runs := map[pair]int64{}, map[pair]int64{}
	for _, h := range hours {
		k := pair{h.workflow, h.version}
		if v, ok := first[k]; !ok || h.firstMs < v {
			first[k] = h.firstMs
		}
		runs[k] += h.runs
	}
	var candidates []model.NewVersion
	recent := map[string]int{}
	for k, ms := range first {
		if ms < since.UnixMilli() || seen[k] {
			continue
		}
		candidates = append(candidates, model.NewVersion{Workflow: k[0], Version: k[1], FirstSeenMs: ms, Runs: runs[k]})
		if ms >= end.Add(-24*time.Hour).UnixMilli() {
			recent[k[0]]++
		}
	}
	var out []model.NewVersion
	for _, c := range candidates {
		if recent[c.Workflow] > maxNewVersions {
			continue
		}
		before := map[string]int64{}
		for _, h := range hours {
			if h.workflow == c.Workflow && h.version != c.Version && h.hourMs < c.FirstSeenMs && h.hourMs >= c.FirstSeenMs-24*3600_000 {
				before[h.version] += h.runs
			}
		}
		for v, n := range before {
			if n > before[c.PrevVersion] || (n == before[c.PrevVersion] && v < c.PrevVersion) {
				c.PrevVersion = v
			}
		}
		if c.PrevVersion != "" {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b model.NewVersion) int {
		return cmp.Or(cmp.Compare(a.FirstSeenMs, b.FirstSeenMs), cmp.Compare(a.Workflow, b.Workflow), cmp.Compare(a.Version, b.Version))
	})
	return out
}

// VersionRunStats sums one workflow version's runs that started in [from, to).
func (r *ClickHouseRepository) VersionRunStats(ctx context.Context, f MetricsFilter, workflow, version string, from, to time.Time) (model.VersionStats, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	clause, args := window("", f.WorkspaceIDs, from.UnixMilli(), to.UnixMilli())
	q := fmt.Sprintf(`
SELECT toInt64(count()), toInt64(countIf(errored)),
    toInt64(sum(truncated)), toInt64(sum(refused)), toInt64(sum(llm)),
    toInt64(sum(tool_errors)), toInt64(sum(tools)),
    quantile(0.5)(cost), quantile(0.5)(dur), quantile(0.95)(dur),
    quantile(0.5)(llm), quantile(0.5)(output), toFloat64(sum(cache_read)), toFloat64(sum(input))
FROM (
    SELECT
        %s AS name,
        %s AS version,
        max(`+spanErrored+`)                           AS errored,
        countIf(silent_failure = 'truncated')            AS truncated,
        countIf(silent_failure = 'refused')              AS refused,
        countIf(kind = 'llm' OR finish_reason != '')     AS llm,
        countIf(silent_failure = 'tool_error')           AS tool_errors,
        countIf(kind = 'tool')                           AS tools,
        sum(cost_usd)                                    AS cost,
        `+traceDurationExpr+`                            AS dur,
        sum(output_tokens)                               AS output,
        sumIf(cache_read_tokens, kind = 'llm')           AS cache_read,
        sumIf(input_tokens, kind = 'llm')                AS input
    FROM `+tableCalls+`
    WHERE %s
    GROUP BY trace_id
)
WHERE name = ? AND version = ?`, workflowExpr, versionExpr, clause)

	var s model.VersionStats
	err := r.db.QueryRowContext(ctx, q, append(args, workflow, version)...).Scan(&s.Runs, &s.ErrorRuns,
		&s.TruncatedCalls, &s.RefusedCalls, &s.LLMCalls, &s.ToolErrors, &s.ToolCalls,
		&s.CostPerRunP50, &s.LatencyP50, &s.LatencyP95, &s.LLMCallsPerRunP50, &s.OutputTokensPerRunP50, &s.CacheRead, &s.InputTokens)
	if err != nil {
		return s, fmt.Errorf("clickhouse: version run stats: %w", err)
	}
	for _, v := range []*float64{&s.CostPerRunP50, &s.LatencyP50, &s.LatencyP95, &s.LLMCallsPerRunP50, &s.OutputTokensPerRunP50} {
		*v = sanitize(*v)
	}
	return s, nil
}
