package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/tracium/api/internal/model"
)

// WorkflowVersions lists f.Workflow's versions in the window, most recently seen first.
func (r *ClickHouseRepository) WorkflowVersions(ctx context.Context, f MetricsFilter, limit int) ([]model.WorkflowVersion, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	return r.workflowVersions(ctx, f, nil, limit)
}

// CompareVersions compares two versions of f.Workflow, or returns ErrNotFound when either has no runs.
func (r *ClickHouseRepository) CompareVersions(ctx context.Context, f MetricsFilter, base, candidate string) (model.VersionComparison, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	versions, err := r.workflowVersions(ctx, f, []string{base, candidate}, 2)
	if err != nil {
		return model.VersionComparison{}, err
	}
	byVersion := make(map[string]model.WorkflowVersion, len(versions))
	for _, v := range versions {
		byVersion[v.Version] = v
	}
	b, okBase := byVersion[base]
	c, okCandidate := byVersion[candidate]
	if !okBase || !okCandidate {
		return model.VersionComparison{}, ErrNotFound
	}
	return compareVersions(b, c), nil
}

func compareVersions(base, candidate model.WorkflowVersion) model.VersionComparison {
	return model.VersionComparison{
		Base:         base,
		Candidate:    candidate,
		CostPerRun:   kpi(candidate.CostPerRun, base.CostPerRun, true),
		P95LatencyMs: kpi(valueOrZero(candidate.P95LatencyMs), valueOrZero(base.P95LatencyMs), true),
		ErrorRate:    kpi(candidate.ErrorRate, base.ErrorRate, true),
		TokensPerRun: kpi(candidate.TokensPerRun, base.TokensPerRun, true),
	}
}

func valueOrZero(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func (r *ClickHouseRepository) workflowVersions(ctx context.Context, f MetricsFilter, only []string, limit int) ([]model.WorkflowVersion, error) {
	if f.UseRollup() {
		return r.workflowVersionsRollup(ctx, f, only, limit)
	}
	clause, args := window(f.UserID, f.WorkspaceIDs, f.Start.UnixMilli(), f.End.UnixMilli())
	onlyClause, onlyArgs := versionIn("version", only)
	q := fmt.Sprintf(`
SELECT
    version,
    any(src),
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
        %s AS src,
        min(start_time_ms)   AS ts,
        sum(cost_usd)        AS cost,
        `+traceDurationExpr+` AS dur,
        max(`+spanErrored+`) AS errored,
        sum(input_tokens + output_tokens) AS tokens
    FROM `+tableCalls+`
    WHERE %s
    GROUP BY trace_id
)
WHERE name = ?%s
GROUP BY version
ORDER BY max(ts) DESC
LIMIT ?`, workflowExpr, versionExpr, versionSourceExpr, clause, onlyClause)

	qArgs := append(args, f.Workflow)
	qArgs = append(qArgs, onlyArgs...)
	return r.scanVersions(ctx, q, append(qArgs, limit), true)
}

// workflowVersionsRollup keys versions per span, so it skips unversioned spans
// without a model: a run versioned only on its LLM calls (a prompt version) would
// otherwise also count as unversioned through its structural spans.
func (r *ClickHouseRepository) workflowVersionsRollup(ctx context.Context, f MetricsFilter, only []string, limit int) ([]model.WorkflowVersion, error) {
	clause, args := rollupWindow(f.UserID, f.WorkspaceIDs, f.Start, f.End, true)
	onlyClause, onlyArgs := versionIn("workflow_version", only)
	q := fmt.Sprintf(`SELECT workflow_version,
    any(version_source),
    min(%[1]s),
    max(%[1]s),
    toInt64(uniqMerge(runs)),
    sum(cost),
    toFloat64(0),
    toInt64(uniqIfMerge(error_runs)),
    toInt64(sum(input_tokens) + sum(output_tokens))
FROM tracium.metrics_daily WHERE %[2]s AND workflow_name = ? AND (workflow_version != '' OR model != '')%[3]s
GROUP BY workflow_version ORDER BY max(bucket_date) DESC LIMIT ?`, bucketMsExpr, clause, onlyClause)

	qArgs := append(args, f.Workflow)
	qArgs = append(qArgs, onlyArgs...)
	return r.scanVersions(ctx, q, append(qArgs, limit), false)
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
		if err := rows.Scan(&v.Version, &v.Source, &v.FirstSeenMs, &v.LastSeenMs,
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

func versionIn(col string, versions []string) (string, []any) {
	if len(versions) == 0 {
		return "", nil
	}
	args := make([]any, len(versions))
	for i, v := range versions {
		args[i] = v
	}
	return " AND " + col + " IN (" + strings.TrimSuffix(strings.Repeat("?,", len(versions)), ",") + ")", args
}
