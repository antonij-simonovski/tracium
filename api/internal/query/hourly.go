package query

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/tracium/api/internal/anomaly"
	"github.com/tracium/api/internal/model"
)

// Hourly checks read tracium.spans over the window's last week plus a day of
// baseline (a week for per-run checks, which fall back to it when a day holds
// too few runs): per-run checks group by trace_id, which no insert-time rollup can.
const (
	hourlyBaselineHours = 24
	runFallbackHours    = 7 * 24
	hourlyLookback      = 7 * 24 * time.Hour
	heartbeatHistory    = 28 * 24 * time.Hour
	stuckAfter          = 30 * time.Minute
	contextHigh         = 0.85
)

var (
	rateOpts = anomaly.RateOptions{
		BaselineHours: hourlyBaselineHours, MinHits: 5, MinRate: 0.05, RateFloor: 0.01,
		Bands: anomaly.Bands{Info: 3, Warning: 5, Critical: 10},
	}
	callsOpts = anomaly.RunOptions{
		BaselineHours: hourlyBaselineHours, MinBaseRuns: 20, FallbackBaselineHours: runFallbackHours,
		AbsoluteMax: 50, MinMax: 10,
		Bands: anomaly.Bands{Info: 3, Warning: 5, Critical: 10},
	}
	toolRepeatOpts = anomaly.RunOptions{
		BaselineHours: hourlyBaselineHours, MinBaseRuns: 20, FallbackBaselineHours: runFallbackHours,
		AbsoluteMax: 30, MinMax: 10,
		Bands: anomaly.Bands{Info: 3, Warning: 5, Critical: 10},
	}
	cacheOpts = anomaly.DropOptions{
		BaselineHours: hourlyBaselineHours, MinBase: 0.2, MinDen: 10_000,
		Bands: anomaly.Bands{Info: 1.6, Warning: 2.5, Critical: 6},
	}
	silenceOpts = anomaly.SilenceOptions{
		MinDays: 4, MinSilenceMs: 3 * anomaly.HourMs,
		MissedBands: anomaly.Bands{Info: 3, Warning: 6, Critical: 12},
		Bands:       anomaly.Bands{Info: 2, Warning: 4, Critical: 8},
	}
	serviceSilenceOpts = func() anomaly.SilenceOptions {
		o := silenceOpts
		o.BusyHours, o.BusyMinSilenceMs = 20, anomaly.HourMs
		o.BusyMissedBands = anomaly.Bands{Info: 1, Warning: 3, Critical: 6}
		return o
	}()
	rejectedOpts = anomaly.VolumeOptions{
		BaselineHours: hourlyBaselineHours, MinCount: 20,
		Bands: anomaly.Bands{Info: 5, Warning: 5, Critical: 20},
	}
	stuckOpts = anomaly.RateOptions{
		BaselineHours: hourlyBaselineHours, MinHits: 3, MinRate: 0.05, RateFloor: 0.01,
		MinBaseTotal: 20, MaxBaseRate: 0.05,
		Bands: anomaly.Bands{Info: 3, Warning: 5, Critical: 10},
	}
	contextTrendOpts = anomaly.CrossOptions{LagHours: 7 * 24, MinN: 10, Above: contextHigh, Below: 0.7}
	modelMixOpts     = anomaly.ShareOptions{
		BaselineHours: hourlyBaselineHours, MinBaseTotal: 20, MaxBaseShare: 0.02, MinShare: 0.2, MinCount: 10, Critical: 0.5,
	}
	// A workflow that always runs near its context limit, or always on a deployment
	// name, has no baseline in its first hour and must not fire then.
	seasonedRateOpts = func() anomaly.RateOptions {
		o := rateOpts
		o.MinBaseTotal = 20
		return o
	}()
	duplicateOpts = anomaly.DuplicateOptions{
		BaselineHours: hourlyBaselineHours, MinBaseTotal: 20, MinCalls: 10, MinInputs: 5,
		MinShare: 0.3, MaxBaseShare: 0.1, Critical: 0.7,
	}
	presenceOpts = anomaly.PresenceOptions{
		BaselineHours: hourlyBaselineHours, FallbackBaselineHours: runFallbackHours,
		MinBaseRuns: 20, MinRuns: 5, Expected: 0.9, TopSteps: 30, Warning: 0.5, Critical: 0.1,
	}
)

type hourAgg struct {
	llm, truncated, refused, tools, toolErrors int64
	cacheRead, input                           float64
	ctxCalls, ctxHigh, hashed                  int64
	ctxP95                                     float64
}

type runAgg struct {
	runs, toolRuns       int64
	callsP50, callsMax   float64
	repP95, repMax       float64
	callsWorst, repWorst string
	repTool              string
	// Runs with a root span, and how many of them ran each step.
	doneRuns int64
	steps    map[string]int64
	// Traces whose newest span is older than stuckAfter, and those with no root.
	settled, stuck int64
	stuckIDs       []string
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
	end, from := f.End.UnixMilli(), hourlyFrom(f)
	lo := from - hourlyBaselineHours*anomaly.HourMs
	axis := hourAxis(lo, end)
	runLo := from - runFallbackHours*anomaly.HourMs
	runAxis := hourAxis(runLo, end)

	var out []model.Anomaly
	keep := func(a model.Anomaly, run, tool string) {
		if severityRank(a.Severity) >= minSev && wants(f, a.Metric) {
			if a.Summary == "" {
				a.Summary = hourlySummary(a, run, tool)
			}
			if run != "" {
				a.TraceIDs = []string{run}
			}
			out = append(out, a)
		}
	}

	var hashed map[string]map[int64]hourAgg
	if wants(f, "truncation_rate", "refusal_rate", "tool_error_rate", "cache_hit_ratio", "context_pressure", "duplicate_output") {
		// From runLo: the context trend compares an hour with the same hour a week earlier.
		byWorkflow, err := r.hourlyFlags(ctx, f, runLo, end)
		if err != nil {
			return nil, err
		}
		hashed = byWorkflow
		for _, name := range sortedKeys(byWorkflow) {
			hours := byWorkflow[name]
			counts := func(pick func(hourAgg) (int64, int64)) []anomaly.Count {
				s := make([]anomaly.Count, len(runAxis))
				for i, h := range runAxis {
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
			spiked := map[int64]bool{}
			so := seasonedRateOpts
			so.DetectFromMs = from
			for _, res := range anomaly.RateSpike(counts(func(a hourAgg) (int64, int64) { return a.ctxHigh, a.ctxCalls }), so) {
				spiked[res.BucketMs] = true
				keep(hourlyAnomaly("context_pressure", name, res), "", "")
			}
			levels := make([]anomaly.Level, len(runAxis))
			for i, h := range runAxis {
				levels[i] = anomaly.Level{BucketMs: h, N: hours[h].ctxCalls, Value: hours[h].ctxP95}
			}
			to := contextTrendOpts
			to.DetectFromMs = from
			for _, res := range anomaly.LevelCross(levels, to) {
				if !spiked[res.BucketMs] {
					a := hourlyAnomaly("context_pressure", name, res)
					a.Summary = contextTrendSummary(a)
					keep(a, "", "")
				}
			}
			ratios := make([]anomaly.Ratio, len(runAxis))
			for i, h := range runAxis {
				ratios[i] = anomaly.Ratio{BucketMs: h, Num: hours[h].cacheRead, Den: hours[h].input}
			}
			co := cacheOpts
			co.DetectFromMs = from
			for _, res := range anomaly.RatioDrop(ratios, co) {
				keep(hourlyAnomaly("cache_hit_ratio", name, res), "", "")
			}
		}
	}

	if wants(f, "model_mix") {
		byWorkflow, err := r.hourlyModels(ctx, f, lo, end)
		if err != nil {
			return nil, err
		}
		for _, name := range sortedKeys(byWorkflow) {
			hours := byWorkflow[name]
			shares := make([]anomaly.Shares, len(axis))
			mismatch := make([]anomaly.Count, len(axis))
			for i, h := range axis {
				a := hours[h]
				shares[i] = anomaly.Shares{BucketMs: h, Total: a.calls, By: a.byModel}
				mismatch[i] = anomaly.Count{BucketMs: h, Hits: a.mismatched, Total: a.responded}
			}
			o := modelMixOpts
			o.DetectFromMs = from
			shifted := map[int64]bool{}
			for _, res := range anomaly.ShareShift(shares, o) {
				shifted[res.BucketMs] = true
				a := hourlyAnomaly("model_mix", name, res.Result)
				a.Summary = modelMixSummary(a, res.Key)
				keep(a, "", "")
			}
			mo := seasonedRateOpts
			mo.DetectFromMs = from
			for _, res := range anomaly.RateSpike(mismatch, mo) {
				if !shifted[res.BucketMs] {
					a := hourlyAnomaly("model_mix", name, res)
					a.Summary = modelMismatchSummary(a, hours[res.BucketMs].topPair)
					keep(a, "", "")
				}
			}
		}
	}

	if wants(f, "duplicate_output") {
		byWorkflow, err := r.hourlyOutputs(ctx, f, lo, end)
		if err != nil {
			return nil, err
		}
		for _, name := range sortedKeys(hashed) {
			series := make([]anomaly.Outputs, len(axis))
			for i, h := range axis {
				series[i] = anomaly.Outputs{BucketMs: h, Total: hashed[name][h].hashed, Repeats: byWorkflow[name][h].repeats}
			}
			o := duplicateOpts
			o.DetectFromMs = from
			for _, res := range anomaly.DuplicateShare(series, o) {
				keep(hourlyAnomaly("duplicate_output", name, res.Result), byWorkflow[name][res.BucketMs].traces[res.Hash], "")
			}
		}
	}

	if wants(f, "llm_calls_per_run", "tool_repeat", "missing_step", "stuck_runs") {
		byWorkflow, err := r.hourlyRuns(ctx, f, runLo, end)
		if err != nil {
			return nil, err
		}
		for _, name := range sortedKeys(byWorkflow) {
			hours := byWorkflow[name]
			calls := make([]anomaly.RunStat, len(runAxis))
			reps := make([]anomaly.RunStat, len(runAxis))
			for i, h := range runAxis {
				a := hours[h]
				calls[i] = anomaly.RunStat{BucketMs: h, Runs: a.runs, Typical: a.callsP50, Max: a.callsMax}
				reps[i] = anomaly.RunStat{BucketMs: h, Runs: a.toolRuns, Typical: a.repP95, Max: a.repMax}
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

			presence := make([]anomaly.Presence, len(runAxis))
			stuck := make([]anomaly.Count, len(runAxis))
			for i, h := range runAxis {
				a := hours[h]
				presence[i] = anomaly.Presence{BucketMs: h, Runs: a.doneRuns, Steps: a.steps}
				stuck[i] = anomaly.Count{BucketMs: h, Hits: a.stuck, Total: a.settled}
			}
			po := presenceOpts
			po.DetectFromMs = from
			for _, res := range anomaly.PresenceDrop(presence, po) {
				a := hourlyAnomaly("missing_step", name, res.Result)
				a.Summary = missingStepSummary(a, res.Step, res.Others)
				keep(a, "", "")
			}
			so := stuckOpts
			so.DetectFromMs = from
			for _, res := range anomaly.RateSpike(stuck, so) {
				a := hourlyAnomaly("stuck_runs", name, res)
				a.TraceIDs = hours[res.BucketMs].stuckIDs
				keep(a, "", "")
			}
		}
	}

	if f.UserID == "" && wants(f, "ingest_silence") {
		services, err := r.serviceActivity(ctx, f)
		if err != nil {
			return nil, err
		}
		o := serviceSilenceOpts
		o.NowMs = end
		silent := anomaly.Silence([]anomaly.Activity{workspaceActivity(services)}, o)
		if len(silent) == 0 {
			silent = anomaly.Silence(services, o)
		}
		for _, res := range silent {
			a := hourlyAnomaly("ingest_silence", "", res.Result)
			a.Scope, a.Service = "workspace", res.Client
			if res.Client != "" {
				a.Scope = "service"
			}
			a.Summary = ingestSilenceSummary(a, res)
			keep(a, "", "")
		}
	}

	if f.UserID == "" && wants(f, "ingest_rejected") {
		counts, codes, err := r.rejectedHourly(ctx, f, lo, end)
		if err != nil {
			return nil, err
		}
		series := make([]anomaly.Volume, len(axis))
		for i, h := range axis {
			series[i] = anomaly.Volume{BucketMs: h, N: counts[h]}
		}
		o := rejectedOpts
		o.DetectFromMs = from
		for _, res := range anomaly.VolumeSpike(series, o) {
			a := hourlyAnomaly("ingest_rejected", "", res)
			a.Scope = "workspace"
			a.Summary = rejectedSummary(a, codes[res.BucketMs])
			keep(a, "", "")
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
			if res.Missed > 0 {
				a.Summary = fmt.Sprintf("Client %q has missed %d expected hours of activity; last seen %s.", res.Client, res.Missed,
					time.UnixMilli(res.LastSeenMs).UTC().Format("2006-01-02 15:04 UTC"))
			}
			keep(a, "", "")
		}
	}
	return out, nil
}

// hourlyFrom is the first hour hourly checks report: the window's last week at most.
func hourlyFrom(f MetricsFilter) int64 {
	from := max(f.Start.UnixMilli(), f.End.UnixMilli()-hourlyLookback.Milliseconds())
	return from / anomaly.HourMs * anomaly.HourMs
}

func hourAxis(lo, hi int64) []int64 {
	axis := make([]int64, 0, (hi-lo)/anomaly.HourMs+1)
	for h := lo; h < hi; h += anomaly.HourMs {
		axis = append(axis, h)
	}
	return axis
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
    toFloat64(sumIf(input_tokens, kind = 'llm'))          AS input,
    toInt64(countIf(kind = 'llm' AND context_ratio > 0))  AS ctx_calls,
    toInt64(countIf(kind = 'llm' AND context_ratio >= ?)) AS ctx_high,
    toFloat64(quantileIf(0.95)(context_ratio, kind = 'llm' AND context_ratio > 0)) AS ctx_p95,
    toInt64(countIf(output_hash != 0))                    AS hashed
FROM tracium.spans WHERE source = 'span' AND workflow_name != '' AND %s
GROUP BY workflow_name, h`, clause)

	rows, err := r.db.QueryContext(ctx, q, append([]any{contextHigh}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: hourly flags: %w", err)
	}
	defer rows.Close()
	out := make(map[string]map[int64]hourAgg)
	for rows.Next() {
		var name string
		var h int64
		var a hourAgg
		if err := rows.Scan(&name, &h, &a.llm, &a.truncated, &a.refused, &a.tools, &a.toolErrors, &a.cacheRead, &a.input,
			&a.ctxCalls, &a.ctxHigh, &a.ctxP95, &a.hashed); err != nil {
			return nil, fmt.Errorf("clickhouse: scan hourly flags: %w", err)
		}
		a.ctxP95 = sanitize(a.ctxP95)
		if out[name] == nil {
			out[name] = make(map[int64]hourAgg)
		}
		out[name][h] = a
	}
	return out, rows.Err()
}

// hourlyRuns rolls spans up to runs, then summarizes each workflow-hour by LLM
// calls per run (median) and by the most calls of one tool in a run (p95). A run belongs to
// the hour and workflow of its first span.
func (r *ClickHouseRepository) hourlyRuns(ctx context.Context, f MetricsFilter, lo, hi int64) (map[string]map[int64]runAgg, error) {
	clause, args := window(f.UserID, f.WorkspaceIDs, lo, hi)
	q := fmt.Sprintf(`SELECT wf, h,
    toInt64(countIf(calls > 0))                    AS runs,
    toFloat64(quantileExactIf(0.5)(calls, calls > 0)) AS calls_p50,
    toFloat64(max(calls))                          AS calls_max,
    argMax(trace_id, calls)                        AS calls_worst,
    toInt64(countIf(rep > 0))                      AS tool_runs,
    toFloat64(quantileExactIf(0.95)(rep, rep > 0)) AS rep_p95,
    toFloat64(max(rep))                            AS rep_max,
    argMax(trace_id, rep)                          AS rep_worst,
    argMax(tool, rep)                              AS rep_tool,
    toInt64(countIf(has_root))                     AS done_runs,
    sumMap(steps, arrayMap(x -> toUInt64(1), steps)).1 AS step_names,
    arrayMap(x -> toInt64(x), sumMap(steps, arrayMap(x -> toUInt64(1), steps)).2) AS step_runs,
    toInt64(countIf(settled))                      AS settled_runs,
    toInt64(countIf(settled AND NOT has_root))     AS stuck_runs,
    groupArrayIf(3)(trace_id, settled AND NOT has_root) AS stuck_ids
FROM (
    SELECT trace_id,
        argMin(workflow_name, start_time_ms)          AS wf,
        intDiv(min(start_time_ms), 3600000) * 3600000 AS h,
        countIf(kind = 'llm')                         AS calls,
        groupArrayIf(name, kind = 'tool')             AS tools,
        arrayDistinct(tools)                          AS names,
        arrayMap(n -> countEqual(tools, n), names)    AS counts,
        if(empty(counts), 0, arrayMax(counts))        AS rep,
        if(empty(counts), '', names[indexOf(counts, rep)]) AS tool,
        countIf(parent_span_id = '') > 0              AS has_root,
        max(end_time_ms) < ?                          AS settled,
        if(has_root, arrayFilter(n -> NOT match(n, '[0-9]{6}|[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-'), groupUniqArrayIf(name, kind != 'llm')), []) AS steps
    FROM tracium.spans WHERE source = 'span' AND %s
    GROUP BY trace_id
)
WHERE wf != ''
GROUP BY wf, h`, clause)

	rows, err := r.db.QueryContext(ctx, q, append([]any{hi - stuckAfter.Milliseconds()}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: hourly runs: %w", err)
	}
	defer rows.Close()
	out := make(map[string]map[int64]runAgg)
	for rows.Next() {
		var name string
		var h int64
		var a runAgg
		var stepNames []string
		var stepRuns []int64
		if err := rows.Scan(&name, &h, &a.runs, &a.callsP50, &a.callsMax, &a.callsWorst,
			&a.toolRuns, &a.repP95, &a.repMax, &a.repWorst, &a.repTool,
			&a.doneRuns, &stepNames, &stepRuns, &a.settled, &a.stuck, &a.stuckIDs); err != nil {
			return nil, fmt.Errorf("clickhouse: scan hourly runs: %w", err)
		}
		a.steps = make(map[string]int64, len(stepNames))
		for i, n := range stepNames {
			a.steps[n] = stepRuns[i]
		}
		a.callsP50, a.repP95 = sanitize(a.callsP50), sanitize(a.repP95)
		if out[name] == nil {
			out[name] = make(map[int64]runAgg)
		}
		out[name][h] = a
	}
	return out, rows.Err()
}

type modelAgg struct {
	calls, responded, mismatched int64
	byModel                      map[string]int64
	// topPair is the most common "requested → answered" pair among mismatches.
	topPair string
	pairN   int64
}

// Snapshot and version suffixes (-2024-08-06, -20241022, -0613, -v2:0, -latest)
// name the same model family.
var modelSuffix = regexp.MustCompile(`([-@](\d{4}-\d{2}-\d{2}|\d{8}|\d{3,4}|v\d+(:\d+)?|latest|preview))+$`)

func modelFamily(m string) string {
	return modelSuffix.ReplaceAllString(m[strings.LastIndex(m, "/")+1:], "")
}

// hourlyModels counts LLM calls per workflow and hour by requested model, and
// those answered by a model of another family.
func (r *ClickHouseRepository) hourlyModels(ctx context.Context, f MetricsFilter, lo, hi int64) (map[string]map[int64]modelAgg, error) {
	clause, args := window(f.UserID, f.WorkspaceIDs, lo, hi)
	q := fmt.Sprintf(`SELECT workflow_name, intDiv(start_time_ms, 3600000) * 3600000 AS h, model_normalized, response_model, toInt64(count())
FROM tracium.spans WHERE source = 'span' AND kind = 'llm' AND workflow_name != '' AND %s
GROUP BY workflow_name, h, model_normalized, response_model`, clause)
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: hourly models: %w", err)
	}
	defer rows.Close()
	out := make(map[string]map[int64]modelAgg)
	for rows.Next() {
		var name, requested, answered string
		var h, n int64
		if err := rows.Scan(&name, &h, &requested, &answered, &n); err != nil {
			return nil, fmt.Errorf("clickhouse: scan hourly models: %w", err)
		}
		if out[name] == nil {
			out[name] = make(map[int64]modelAgg)
		}
		a := out[name][h]
		if a.byModel == nil {
			a.byModel = map[string]int64{}
		}
		a.calls += n
		a.byModel[requested] += n
		if requested != "" && answered != "" {
			a.responded += n
			if modelFamily(requested) != modelFamily(answered) {
				a.mismatched += n
				if n > a.pairN {
					a.topPair, a.pairN = requested+" answered by "+answered, n
				}
			}
		}
		out[name][h] = a
	}
	return out, rows.Err()
}

type outputAgg struct {
	repeats map[uint64]anomaly.Repeat
	traces  map[uint64]string
}

// hourlyOutputs finds, per workflow and hour, the outputs returned for more
// than one input: the calls not on its most common input, its distinct inputs,
// and one trace that returned it.
func (r *ClickHouseRepository) hourlyOutputs(ctx context.Context, f MetricsFilter, lo, hi int64) (map[string]map[int64]outputAgg, error) {
	clause, args := window(f.UserID, f.WorkspaceIDs, lo, hi)
	q := fmt.Sprintf(`SELECT wf, h, oh, toInt64(sum(c) - max(c)) AS dup, toInt64(count()) AS inputs, any(t)
FROM (
    SELECT workflow_name AS wf, intDiv(start_time_ms, 3600000) * 3600000 AS h, output_hash AS oh, input_hash,
        count() AS c, any(trace_id) AS t
    FROM tracium.spans WHERE source = 'span' AND output_hash != 0 AND workflow_name != '' AND %s
    GROUP BY wf, h, oh, input_hash
)
GROUP BY wf, h, oh HAVING inputs > 1`, clause)
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: hourly outputs: %w", err)
	}
	defer rows.Close()
	out := make(map[string]map[int64]outputAgg)
	for rows.Next() {
		var name, trace string
		var h int64
		var hash uint64
		var rep anomaly.Repeat
		if err := rows.Scan(&name, &h, &hash, &rep.Dup, &rep.Inputs, &trace); err != nil {
			return nil, fmt.Errorf("clickhouse: scan hourly outputs: %w", err)
		}
		if out[name] == nil {
			out[name] = make(map[int64]outputAgg)
		}
		a, ok := out[name][h]
		if !ok {
			a = outputAgg{repeats: map[uint64]anomaly.Repeat{}, traces: map[uint64]string{}}
			out[name][h] = a
		}
		a.repeats[hash], a.traces[hash] = rep, trace
	}
	return out, rows.Err()
}

// clientActivity loads each client's active hours over the last four weeks
// from the hourly activity rollup: at most 672 rows per client.
func (r *ClickHouseRepository) clientActivity(ctx context.Context, f MetricsFilter) ([]anomaly.Activity, error) {
	clause := "dim = 'client' AND hour >= toDateTime(?) AND hour < toDateTime(?)"
	args := []any{f.End.Add(-heartbeatHistory).Unix(), f.End.Unix()}
	if f.UserID != "" {
		clause += " AND key = ?"
		args = append(args, f.UserID)
	}
	wsClause, wsArgs := workspaceScope(f.WorkspaceIDs)
	q := `SELECT key, groupUniqArray(toInt64(toUnixTimestamp(hour)) * 1000) FROM tracium.activity_hourly
WHERE ` + clause + wsClause + ` GROUP BY key ORDER BY key`
	var out []anomaly.Activity
	if err := r.scanBuckets(ctx, q, append(args, wsArgs...), func(c string, b []int64) {
		out = append(out, anomaly.Activity{Client: c, Hours: b})
	}); err != nil {
		return nil, fmt.Errorf("clickhouse: client activity: %w", err)
	}
	return out, nil
}

// serviceActivity loads each service's active hours over the last four weeks.
func (r *ClickHouseRepository) serviceActivity(ctx context.Context, f MetricsFilter) ([]anomaly.Activity, error) {
	wsClause, wsArgs := workspaceScope(f.WorkspaceIDs)
	q := `SELECT key, groupUniqArray(toInt64(toUnixTimestamp(hour)) * 1000) FROM tracium.activity_hourly
WHERE dim = 'service' AND hour >= toDateTime(?) AND hour < toDateTime(?)` + wsClause + ` GROUP BY key ORDER BY key`
	var out []anomaly.Activity
	if err := r.scanBuckets(ctx, q, append([]any{f.End.Add(-heartbeatHistory).Unix(), f.End.Unix()}, wsArgs...), func(c string, b []int64) {
		out = append(out, anomaly.Activity{Client: c, Hours: b})
	}); err != nil {
		return nil, fmt.Errorf("clickhouse: service activity: %w", err)
	}
	return out, nil
}

// workspaceActivity is the hours any service was active, keyed "".
func workspaceActivity(services []anomaly.Activity) anomaly.Activity {
	seen := map[int64]bool{}
	var hours []int64
	for _, s := range services {
		for _, h := range s.Hours {
			if !seen[h] {
				seen[h] = true
				hours = append(hours, h)
			}
		}
	}
	return anomaly.Activity{Hours: hours}
}

// rejectedHourly sums rejected spans per hour, with the most rejected code of each hour.
func (r *ClickHouseRepository) rejectedHourly(ctx context.Context, f MetricsFilter, lo, hi int64) (map[int64]int64, map[int64]string, error) {
	wsClause, wsArgs := workspaceScope(f.WorkspaceIDs)
	q := `SELECT h, toInt64(sum(n)), argMax(code, n) FROM (
    SELECT toInt64(toUnixTimestamp(hour)) * 1000 AS h, code, sum(spans) AS n FROM tracium.rejected_spans
    WHERE hour >= toDateTime(?) AND hour < toDateTime(?)` + wsClause + `
    GROUP BY h, code
) GROUP BY h`
	rows, err := r.db.QueryContext(ctx, q, append([]any{lo / 1000, hi / 1000}, wsArgs...)...)
	if err != nil {
		return nil, nil, fmt.Errorf("clickhouse: rejected hourly: %w", err)
	}
	defer rows.Close()
	counts, codes := map[int64]int64{}, map[int64]string{}
	for rows.Next() {
		var h, n int64
		var code string
		if err := rows.Scan(&h, &n, &code); err != nil {
			return nil, nil, fmt.Errorf("clickhouse: scan rejected hourly: %w", err)
		}
		counts[h], codes[h] = n, code
	}
	return counts, codes, rows.Err()
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
	obs, exp := FormatAnomalyValue(a.Metric, a.Observed), FormatAnomalyValue(a.Metric, a.Expected)
	switch a.Metric {
	case "llm_calls_per_run":
		if a.Expected == 0 {
			return fmt.Sprintf("%s run %s made %s LLM calls in the hour from %s; no baseline yet.", subject, run, obs, at)
		}
		return fmt.Sprintf("%s run %s made %s LLM calls in the hour from %s; its runs usually make %s.", subject, run, obs, at, exp)
	case "tool_repeat":
		if a.Expected == 0 {
			return fmt.Sprintf("%s run %s called tool %q %s times in the hour from %s; no baseline yet.", subject, run, tool, obs, at)
		}
		return fmt.Sprintf("%s run %s called tool %q %s times in the hour from %s; 95%% of its runs call one tool at most %s times.", subject, run, tool, obs, at, exp)
	case "cache_hit_ratio":
		return fmt.Sprintf("%s cache hit ratio fell to %s in the hour from %s, from %s over the prior 24h.", subject, obs, at, exp)
	case "heartbeat":
		return fmt.Sprintf("Client %q has been silent for %s; it usually runs every %s.", a.Client, obs, exp)
	case "context_pressure":
		return fmt.Sprintf("%s: %s of LLM calls used over 85%% of the model's context window in the hour from %s, from %s over the prior 24h.", subject, obs, at, exp)
	case "duplicate_output":
		return fmt.Sprintf("%s: %s of LLM calls returned the same output for different inputs in the hour from %s, from %s over the prior 24h.", subject, obs, at, exp)
	case "stuck_runs":
		return fmt.Sprintf("%s runs that started and never finished rose to %s in the hour from %s, from %s over the prior 24h.", subject, obs, at, exp)
	}
	noun := map[string]string{"truncation_rate": "truncated LLM calls", "refusal_rate": "refused LLM calls", "tool_error_rate": "tool calls that returned an error"}[a.Metric]
	return fmt.Sprintf("%s %s rose to %s in the hour from %s, from %s over the prior 24h.", subject, noun, obs, at, exp)
}

func contextTrendSummary(a model.Anomaly) string {
	return fmt.Sprintf("Workflow %q: p95 LLM call used %s of the model's context window in the hour from %s; it was %s a week earlier.",
		a.Workflow, FormatAnomalyValue(a.Metric, a.Observed), time.UnixMilli(a.BucketMs).UTC().Format("2006-01-02 15:04 UTC"), FormatAnomalyValue(a.Metric, a.Expected))
}

func modelMixSummary(a model.Anomaly, m string) string {
	return fmt.Sprintf("Workflow %q: %s of LLM calls used %s in the hour from %s; it was %s over the prior 24h.",
		a.Workflow, FormatAnomalyValue(a.Metric, a.Observed), m, time.UnixMilli(a.BucketMs).UTC().Format("2006-01-02 15:04 UTC"), FormatAnomalyValue(a.Metric, a.Expected))
}

func modelMismatchSummary(a model.Anomaly, pair string) string {
	return fmt.Sprintf("Workflow %q: %s of LLM calls were answered by another model than requested (%s) in the hour from %s, from %s over the prior 24h.",
		a.Workflow, FormatAnomalyValue(a.Metric, a.Observed), pair, time.UnixMilli(a.BucketMs).UTC().Format("2006-01-02 15:04 UTC"), FormatAnomalyValue(a.Metric, a.Expected))
}

func missingStepSummary(a model.Anomaly, step string, others int) string {
	more := ""
	if others == 1 {
		more = " One more step dropped too."
	} else if others > 1 {
		more = fmt.Sprintf(" %d more steps dropped too.", others)
	}
	return fmt.Sprintf("Workflow %q: step %q ran in %.0f%% of runs in the hour from %s; it normally runs in %.0f%%.%s",
		a.Workflow, step, a.Observed*100, time.UnixMilli(a.BucketMs).UTC().Format("2006-01-02 15:04 UTC"), a.Expected*100, more)
}

func ingestSilenceSummary(a model.Anomaly, res anomaly.SilenceResult) string {
	subject := "The workspace"
	if a.Service != "" {
		subject = fmt.Sprintf("Service %q", a.Service)
	}
	last := time.UnixMilli(res.LastSeenMs).UTC().Format("2006-01-02 15:04 UTC")
	if res.Missed > 0 {
		return fmt.Sprintf("%s has sent no spans in %d hours it usually sends in; last seen %s.", subject, res.Missed, last)
	}
	return fmt.Sprintf("%s has sent no spans for %.0fh; it usually sends every %.0fh.", subject, a.Observed, a.Expected)
}

func rejectedSummary(a model.Anomaly, code string) string {
	msg, ok := rejectedMessages[code]
	if !ok {
		msg = fmt.Sprintf("Spans were rejected at ingest (%s).", code)
	}
	at := time.UnixMilli(a.BucketMs).UTC().Format("2006-01-02 15:04 UTC")
	return fmt.Sprintf("The collector rejected %.0f spans in the hour from %s, against %.1f an hour over the prior 24h. %s", a.Observed, at, a.Expected, msg)
}
