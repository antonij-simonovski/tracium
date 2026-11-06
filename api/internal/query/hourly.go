package query

import (
	"context"
	"fmt"
	"time"

	"github.com/tracium/api/internal/anomaly"
	"github.com/tracium/api/internal/model"
)

// Hourly checks read tracium.spans over the window's last week plus a day of
// baseline: per-run checks group by trace_id, which no insert-time rollup can.
const (
	hourlyBaselineHours = 24
	hourlyLookback      = 7 * 24 * time.Hour
	heartbeatHistory    = 28 * 24 * time.Hour
	heartbeatRecent     = 48 * time.Hour
)

var (
	rateOpts = anomaly.RateOptions{
		BaselineHours: hourlyBaselineHours, MinHits: 5, MinRate: 0.05, RateFloor: 0.01,
		Bands: anomaly.Bands{Info: 3, Warning: 5, Critical: 10},
	}
	callsOpts = anomaly.RunOptions{
		BaselineHours: hourlyBaselineHours, MinBaseRuns: 20, MinMax: 10,
		Bands: anomaly.Bands{Info: 3, Warning: 5, Critical: 10},
	}
	toolRepeatOpts = anomaly.RunOptions{
		BaselineHours: hourlyBaselineHours, MinMax: 10,
		Bands: anomaly.Bands{Info: 3, Warning: 5, Critical: 10},
	}
	cacheOpts = anomaly.DropOptions{
		BaselineHours: hourlyBaselineHours, MinBase: 0.2, MinDen: 10_000,
		Bands: anomaly.Bands{Info: 1.6, Warning: 2.5, Critical: 6},
	}
	silenceOpts = anomaly.SilenceOptions{
		MinDays: 4, MinSilenceMs: 3 * anomaly.HourMs,
		Bands: anomaly.Bands{Info: 2, Warning: 4, Critical: 8},
	}
)

type hourAgg struct {
	llm, truncated, refused, tools, toolErrors int64
	cacheRead, input                           float64
}

type runAgg struct {
	runs, toolRuns       int64
	callsP50, callsMax   float64
	repP50, repMax       float64
	callsWorst, repWorst string
	repTool              string
}

func wants(f MetricsFilter, metrics ...string) bool {
	if f.AnomalyMetric == "" {
		return true
	}
	for _, m := range metrics {
		if m == f.AnomalyMetric {
			return true
		}
	}
	return false
}

func (r *ClickHouseRepository) hourlyAnomalies(ctx context.Context, f MetricsFilter, minSev int) ([]model.Anomaly, error) {
	end := f.End.UnixMilli()
	from := max(f.Start.UnixMilli(), end-hourlyLookback.Milliseconds())
	from = from / anomaly.HourMs * anomaly.HourMs
	lo := from - hourlyBaselineHours*anomaly.HourMs
	axis := make([]int64, 0, (end-lo)/anomaly.HourMs+1)
	for h := lo; h < end; h += anomaly.HourMs {
		axis = append(axis, h)
	}

	var out []model.Anomaly
	keep := func(a model.Anomaly, run, tool string) {
		if severityRank(a.Severity) >= minSev && wants(f, a.Metric) {
			a.Summary = hourlySummary(a, run, tool)
			out = append(out, a)
		}
	}

	if wants(f, "truncation_rate", "refusal_rate", "tool_error_rate", "cache_hit_ratio") {
		byWorkflow, err := r.hourlyFlags(ctx, f, lo, end)
		if err != nil {
			return nil, err
		}
		for _, name := range sortedKeys(byWorkflow) {
			hours := byWorkflow[name]
			counts := func(pick func(hourAgg) (int64, int64)) []anomaly.Count {
				s := make([]anomaly.Count, len(axis))
				for i, h := range axis {
					s[i].BucketMs = h
					s[i].Hits, s[i].Total = pick(hours[h])
				}
				return s
			}
			o := rateOpts
			o.DetectFromMs = from
			for metric, series := range map[string][]anomaly.Count{
				"truncation_rate": counts(func(a hourAgg) (int64, int64) { return a.truncated, a.llm }),
				"refusal_rate":    counts(func(a hourAgg) (int64, int64) { return a.refused, a.llm }),
				"tool_error_rate": counts(func(a hourAgg) (int64, int64) { return a.toolErrors, a.tools }),
			} {
				for _, res := range anomaly.RateSpike(series, o) {
					keep(hourlyAnomaly(metric, name, res), "", "")
				}
			}
			ratios := make([]anomaly.Ratio, len(axis))
			for i, h := range axis {
				ratios[i] = anomaly.Ratio{BucketMs: h, Num: hours[h].cacheRead, Den: hours[h].input}
			}
			co := cacheOpts
			co.DetectFromMs = from
			for _, res := range anomaly.RatioDrop(ratios, co) {
				keep(hourlyAnomaly("cache_hit_ratio", name, res), "", "")
			}
		}
	}

	if wants(f, "llm_calls_per_run", "tool_repeat") {
		byWorkflow, err := r.hourlyRuns(ctx, f, lo, end)
		if err != nil {
			return nil, err
		}
		for _, name := range sortedKeys(byWorkflow) {
			hours := byWorkflow[name]
			calls := make([]anomaly.RunStat, len(axis))
			reps := make([]anomaly.RunStat, len(axis))
			for i, h := range axis {
				a := hours[h]
				calls[i] = anomaly.RunStat{BucketMs: h, Runs: a.runs, Median: a.callsP50, Max: a.callsMax}
				reps[i] = anomaly.RunStat{BucketMs: h, Runs: a.toolRuns, Median: a.repP50, Max: a.repMax}
			}
			o := callsOpts
			o.DetectFromMs = from
			for _, res := range anomaly.RunOutlier(calls, o) {
				keep(hourlyAnomaly("llm_calls_per_run", name, res), hours[res.BucketMs].callsWorst, "")
			}
			o = toolRepeatOpts
			o.DetectFromMs = from
			for _, res := range anomaly.RunOutlier(reps, o) {
				keep(hourlyAnomaly("tool_repeat", name, res), hours[res.BucketMs].repWorst, hours[res.BucketMs].repTool)
			}
		}
	}

	if wants(f, "heartbeat") {
		clients, err := r.clientActivity(ctx, f)
		if err != nil {
			return nil, err
		}
		o := silenceOpts
		o.NowMs = end
		for _, res := range anomaly.Silence(clients, o) {
			a := hourlyAnomaly("heartbeat", "", res.Result)
			a.Scope, a.Client = "client", res.Client
			keep(a, "", "")
		}
	}
	return out, nil
}

func hourlyAnomaly(metric, workflow string, res anomaly.Result) model.Anomaly {
	return model.Anomaly{
		Metric:    metric,
		Scope:     "workflow",
		Workflow:  workflow,
		BucketMs:  res.BucketMs,
		Observed:  sanitize(res.Observed),
		Expected:  sanitize(res.Expected),
		Deviation: sanitize(res.Deviation),
		Score:     sanitize(res.Score),
		Direction: string(res.Direction),
		Severity:  string(res.Severity),
	}
}

// hourlyFlags counts the ingest silent-failure flags and cache reads per
// workflow and hour.
func (r *ClickHouseRepository) hourlyFlags(ctx context.Context, f MetricsFilter, lo, hi int64) (map[string]map[int64]hourAgg, error) {
	clause, args := window(f.UserID, f.WorkspaceIDs, lo, hi)
	q := fmt.Sprintf(`SELECT workflow_name, intDiv(start_time_ms, 3600000) * 3600000 AS h,
    toInt64(countIf(kind = 'llm' OR finish_reason != '')) AS llm,
    toInt64(countIf(silent_failure = 'truncated'))        AS truncated,
    toInt64(countIf(silent_failure = 'refused'))          AS refused,
    toInt64(countIf(kind = 'tool'))                       AS tools,
    toInt64(countIf(silent_failure = 'tool_error'))       AS tool_errors,
    toFloat64(sumIf(cache_read_tokens, kind = 'llm'))     AS cache_read,
    toFloat64(sumIf(input_tokens, kind = 'llm'))          AS input
FROM tracium.spans WHERE source = 'span' AND workflow_name != '' AND %s
GROUP BY workflow_name, h`, clause)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: hourly flags: %w", err)
	}
	defer rows.Close()
	out := make(map[string]map[int64]hourAgg)
	for rows.Next() {
		var name string
		var h int64
		var a hourAgg
		if err := rows.Scan(&name, &h, &a.llm, &a.truncated, &a.refused, &a.tools, &a.toolErrors, &a.cacheRead, &a.input); err != nil {
			return nil, fmt.Errorf("clickhouse: scan hourly flags: %w", err)
		}
		if out[name] == nil {
			out[name] = make(map[int64]hourAgg)
		}
		out[name][h] = a
	}
	return out, rows.Err()
}

// hourlyRuns rolls spans up to runs, then summarizes each workflow-hour by LLM
// calls per run and by the most calls of one tool in a run. A run belongs to
// the hour and workflow of its first span.
func (r *ClickHouseRepository) hourlyRuns(ctx context.Context, f MetricsFilter, lo, hi int64) (map[string]map[int64]runAgg, error) {
	clause, args := window(f.UserID, f.WorkspaceIDs, lo, hi)
	q := fmt.Sprintf(`SELECT wf, h,
    toInt64(countIf(calls > 0))                    AS runs,
    toFloat64(quantileExactIf(0.5)(calls, calls > 0)) AS calls_p50,
    toFloat64(max(calls))                          AS calls_max,
    argMax(trace_id, calls)                        AS calls_worst,
    toInt64(countIf(rep > 0))                      AS tool_runs,
    toFloat64(quantileExactIf(0.5)(rep, rep > 0))  AS rep_p50,
    toFloat64(max(rep))                            AS rep_max,
    argMax(trace_id, rep)                          AS rep_worst,
    argMax(tool, rep)                              AS rep_tool
FROM (
    SELECT trace_id,
        argMin(workflow_name, start_time_ms)          AS wf,
        intDiv(min(start_time_ms), 3600000) * 3600000 AS h,
        countIf(kind = 'llm')                         AS calls,
        groupArrayIf(name, kind = 'tool')             AS tools,
        arrayDistinct(tools)                          AS names,
        arrayMap(n -> countEqual(tools, n), names)    AS counts,
        if(empty(counts), 0, arrayMax(counts))        AS rep,
        if(empty(counts), '', names[indexOf(counts, rep)]) AS tool
    FROM tracium.spans WHERE source = 'span' AND %s
    GROUP BY trace_id
)
WHERE wf != ''
GROUP BY wf, h`, clause)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: hourly runs: %w", err)
	}
	defer rows.Close()
	out := make(map[string]map[int64]runAgg)
	for rows.Next() {
		var name string
		var h int64
		var a runAgg
		if err := rows.Scan(&name, &h, &a.runs, &a.callsP50, &a.callsMax, &a.callsWorst,
			&a.toolRuns, &a.repP50, &a.repMax, &a.repWorst, &a.repTool); err != nil {
			return nil, fmt.Errorf("clickhouse: scan hourly runs: %w", err)
		}
		a.callsP50, a.repP50 = sanitize(a.callsP50), sanitize(a.repP50)
		if out[name] == nil {
			out[name] = make(map[int64]runAgg)
		}
		out[name][h] = a
	}
	return out, rows.Err()
}

// clientActivity loads each client's active days from the daily rollup and its
// active hours over the recent window from spans.
func (r *ClickHouseRepository) clientActivity(ctx context.Context, f MetricsFilter) ([]anomaly.Activity, error) {
	byClient := make(map[string]*anomaly.Activity)
	get := func(c string) *anomaly.Activity {
		if byClient[c] == nil {
			byClient[c] = &anomaly.Activity{Client: c}
		}
		return byClient[c]
	}

	clause, args := rollupWindow(f.UserID, f.WorkspaceIDs, f.End.Add(-heartbeatHistory), f.End, true)
	q := fmt.Sprintf(`SELECT user_id, groupUniqArray(%s) FROM tracium.metrics_daily
WHERE %s AND user_id != '' GROUP BY user_id`, bucketMsExpr, clause)
	if err := r.scanBuckets(ctx, q, args, func(c string, b []int64) { get(c).Days = b }); err != nil {
		return nil, fmt.Errorf("clickhouse: client days: %w", err)
	}

	clause, args = window(f.UserID, f.WorkspaceIDs, f.End.Add(-heartbeatRecent).UnixMilli(), f.End.UnixMilli())
	q = fmt.Sprintf(`SELECT user_id, groupUniqArray(intDiv(start_time_ms, 3600000) * 3600000) FROM tracium.spans
WHERE source = 'span' AND user_id != '' AND %s GROUP BY user_id`, clause)
	if err := r.scanBuckets(ctx, q, args, func(c string, b []int64) { get(c).RecentHours = b }); err != nil {
		return nil, fmt.Errorf("clickhouse: client hours: %w", err)
	}

	out := make([]anomaly.Activity, 0, len(byClient))
	for _, c := range sortedKeys(byClient) {
		out = append(out, *byClient[c])
	}
	return out, nil
}

func (r *ClickHouseRepository) scanBuckets(ctx context.Context, q string, args []any, set func(string, []int64)) error {
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		var b []int64
		if err := rows.Scan(&c, &b); err != nil {
			return err
		}
		set(c, b)
	}
	return rows.Err()
}

func hourlySummary(a model.Anomaly, run, tool string) string {
	subject := fmt.Sprintf("Workflow %q", a.Workflow)
	at := time.UnixMilli(a.BucketMs).UTC().Format("2006-01-02 15:04 UTC")
	obs, exp := formatAnomalyValue(a.Metric, a.Observed), formatAnomalyValue(a.Metric, a.Expected)
	switch a.Metric {
	case "llm_calls_per_run":
		return fmt.Sprintf("%s run %s made %s LLM calls in the hour from %s; its runs usually make %s.", subject, run, obs, at, exp)
	case "tool_repeat":
		return fmt.Sprintf("%s run %s called tool %q %s times in the hour from %s; its runs usually call one tool at most %s times.", subject, run, tool, obs, at, exp)
	case "cache_hit_ratio":
		return fmt.Sprintf("%s cache hit ratio fell to %s in the hour from %s, from %s over the prior 24h.", subject, obs, at, exp)
	case "heartbeat":
		return fmt.Sprintf("Client %q has been silent for %s; it usually runs every %s.", a.Client, obs, exp)
	}
	noun := map[string]string{"truncation_rate": "truncated LLM calls", "refusal_rate": "refused LLM calls", "tool_error_rate": "tool calls that returned an error"}[a.Metric]
	return fmt.Sprintf("%s %s rose to %s in the hour from %s, from %s over the prior 24h.", subject, noun, obs, at, exp)
}
