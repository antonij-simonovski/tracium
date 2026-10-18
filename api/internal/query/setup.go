package query

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/tracium/api/internal/model"
)

// setupCheck defines a check by either the predicate a span fails (cond) or
// the count of affected spans in a trace (agg). cond also drives the issues
// served on each span of a trace.
type setupCheck struct {
	model.SetupIssue
	cond      string
	agg       string
	everySpan bool // reported only when every span in the window is affected
}

// Traces whose first span is within an hour of either window edge are skipped:
// the root ends last, so it is exported last, and it may start before the window.
const missingRootAgg = `if(countIf(parent_span_id = '') = 0 AND min(start_time_ms) BETWEEN window_start + 3600000 AND toUnixTimestamp64Milli(now64()) - 3600000, count(), 0)`

var setupChecks = []setupCheck{
	{
		SetupIssue: model.SetupIssue{Code: "no_llm_spans", Severity: "critical",
			Message: "Spans are arriving, but none of them is an LLM call. Install a GenAI instrumentation such as OpenLLMetry or opentelemetry-instrumentation-openai-v2, or set gen_ai.request.model and the gen_ai.usage.* token attributes on your own spans."},
		cond:      "model = '' AND kind NOT IN ('llm', 'embedding')",
		everySpan: true,
	},
	{
		SetupIssue: model.SetupIssue{Code: "unpriced_model", Severity: "warning",
			Message: "Spans report tokens for a model the price table does not know, so their cost is recorded as $0. Check the model name in gen_ai.request.model, or add the model to the collector's pricing file."},
		cond: "model != '' AND cost_usd = 0 AND (input_tokens > 0 OR output_tokens > 0)",
	},
	{
		SetupIssue: model.SetupIssue{Code: "unmetered", Severity: "warning",
			Message: "Streamed calls arrived without token usage, so their cost is unknown. For OpenAI, pass stream_options={\"include_usage\": True}."},
		cond: "unmetered = 1",
	},
	{
		SetupIssue: model.SetupIssue{Code: "no_model", Severity: "warning",
			Message: "LLM spans arrived without a model name, so they cannot be priced. Set gen_ai.request.model or gen_ai.response.model."},
		cond: "model = '' AND kind IN ('llm', 'embedding')",
	},
	{
		SetupIssue: model.SetupIssue{Code: "instant_llm_spans", Severity: "warning",
			Message: "LLM spans end within milliseconds of starting, so their latency is wrong. This usually means a streamed response is read after the span has ended; end the span once the stream is consumed."},
		cond: "kind = 'llm' AND duration_ms < 10 AND output_tokens > 0",
	},
	{
		SetupIssue: model.SetupIssue{Code: "error_without_detail", Severity: "info",
			Message: "Failed spans carry no exception or status message, so the Failures view cannot say what went wrong. Record the exception on the span, or set a status description."},
		cond: "error_type = 'error' AND error_message = ''",
	},
	{
		SetupIssue: model.SetupIssue{Code: "missing_root_span", Severity: "warning",
			Message: "Traces arrived without their root span, so their runs, latency and workflow names are incomplete. Make sure every service in the call path exports to Tracium."},
		agg: missingRootAgg,
	},
	{
		SetupIssue: model.SetupIssue{Code: "unnamed_workflow", Severity: "warning",
			Message: "Workflows are named after service.name or the span name because their root span has no gen_ai.agent.name. service.name covers the whole application, so different workflows are merged into one. Set gen_ai.agent.name on each workflow's root span."},
		// The collector prefers gen_ai.agent.name and Traceloop's names, and only
		// then falls back to service.name and the span name.
		cond: "parent_span_id = '' AND (workflow_name = service_name OR workflow_name = name)",
	},
	{
		SetupIssue: model.SetupIssue{Code: "workflow_without_version", Severity: "info",
			Message: "Workflow runs arrived without a version, so they cannot be compared across releases. Set the tracium.workflow.version resource attribute (see Workflow versions in the README)."},
		agg: "if(countIf(workflow_name != '') > 0 AND countIf(workflow_version != '') = 0, count(), 0)",
	},
}

// A workflow is flagged, per workspace, when it has at least versionPerRunMinRuns
// versioned runs and more than half of them carry a distinct version.
const versionPerRunMinRuns = 20

var versionPerRunIssue = model.SetupIssue{Code: "version_per_run", Severity: "warning",
	Message: "A workflow reports a new version on almost every run, so its versions cannot be compared. tracium.workflow.version should change per release, not per run; a timestamp, run ID or request ID makes every run its own version."}

// rejectedMessages is keyed by the collector's span drop codes.
var rejectedMessages = map[string]string{
	"invalid_timestamp":        "Spans were rejected because their timestamps are invalid: missing, or ending before they start. Check the sending host's clock.",
	"field_too_long":           "Spans were rejected because their model name is longer than the ingest limit.",
	"token_count_out_of_range": "Spans were rejected because a token count is larger than any real call can produce. Check the gen_ai.usage.* attributes.",
	"unknown_model":            "Spans were rejected because their model is not in the collector's allowed_models list.",
	"missing_trace_id":         "Spans were rejected because they have no trace ID.",
	"missing_span_id":          "Spans were rejected because they have no span ID.",
}

var setupIssueByCode = func() map[string]model.SetupIssue {
	m := make(map[string]model.SetupIssue, len(setupChecks))
	for _, c := range setupChecks {
		m[c.Code] = c.SetupIssue
	}
	return m
}()

var spanIssuesSQL = func() string {
	var ifs []string
	for _, c := range setupChecks {
		if c.cond != "" && !c.everySpan {
			ifs = append(ifs, fmt.Sprintf("if(%s, '%s', '')", c.cond, c.Code))
		}
	}
	return "arrayFilter(x -> x != '', [" + strings.Join(ifs, ", ") + "])"
}()

// setupTallySQL counts each check's affected spans per trace, then sums them
// and keeps one affected trace as an example.
func setupTallySQL(clause string) string {
	inner := []string{"count() AS n"}
	outer := []string{"toInt64(sum(n))"}
	for i, c := range setupChecks {
		agg := c.agg
		if agg == "" {
			agg = "countIf(" + c.cond + ")"
		}
		inner = append(inner, fmt.Sprintf("%s AS c%d", agg, i))
		outer = append(outer, fmt.Sprintf("toInt64(sum(c%d)), anyIf(trace_id, c%d > 0)", i, i))
	}
	return "SELECT " + strings.Join(outer, ", ") +
		" FROM (WITH ? AS window_start SELECT trace_id, " + strings.Join(inner, ", ") +
		" FROM " + tableCalls + " WHERE " + clause + " GROUP BY trace_id)"
}

// SetupChecks returns the checks that affected any stored or rejected span in
// the window, most severe first.
func (r *ClickHouseRepository) SetupChecks(ctx context.Context, f MetricsFilter) ([]model.SetupCheck, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	clause, args := window(f.UserID, f.WorkspaceIDs, f.Start.UnixMilli(), f.End.UnixMilli())
	var total int64
	found := make([]model.SetupCheck, len(setupChecks))
	dest := []any{&total}
	for i, c := range setupChecks {
		found[i].SetupIssue = c.SetupIssue
		dest = append(dest, &found[i].Spans, &found[i].ExampleTraceID)
	}
	if err := r.db.QueryRowContext(ctx, setupTallySQL(clause), append([]any{f.Start.UnixMilli()}, args...)...).Scan(dest...); err != nil {
		return nil, fmt.Errorf("clickhouse: setup checks: %w", err)
	}

	checks := []model.SetupCheck{}
	for i, c := range found {
		if c.Spans > 0 && (!setupChecks[i].everySpan || c.Spans == total) {
			checks = append(checks, c)
		}
	}
	perRun, err := r.versionPerRunCheck(ctx, clause, args)
	if err != nil {
		return nil, err
	}
	if perRun.Spans > 0 {
		checks = append(checks, perRun)
	}
	rejected, err := r.rejectedChecks(ctx, f)
	if err != nil {
		return nil, err
	}
	for _, c := range rejected {
		total += c.Spans
	}
	checks = append(checks, rejected...)

	for i := range checks {
		checks[i].Share = float64(checks[i].Spans) / float64(total)
	}
	sort.SliceStable(checks, func(i, j int) bool {
		si, sj := severityRank(checks[i].Severity), severityRank(checks[j].Severity)
		if si != sj {
			return si > sj
		}
		return checks[i].Spans > checks[j].Spans
	})
	return checks, nil
}

var versionPerRunSQL = fmt.Sprintf(`SELECT toInt64(sum(spans)), any(example) FROM (
    SELECT sum(n) AS spans, any(trace_id) AS example
    FROM (SELECT workspace_id, trace_id, %s AS name, %s AS version, count() AS n FROM %s WHERE %%s GROUP BY workspace_id, trace_id)
    WHERE version != ''
    GROUP BY workspace_id, name
    HAVING count() >= %d AND uniqExact(version) * 2 > count()
)`, workflowExpr, versionExpr, tableCalls, versionPerRunMinRuns)

func (r *ClickHouseRepository) versionPerRunCheck(ctx context.Context, clause string, args []any) (model.SetupCheck, error) {
	c := model.SetupCheck{SetupIssue: versionPerRunIssue}
	if err := r.db.QueryRowContext(ctx, fmt.Sprintf(versionPerRunSQL, clause), args...).Scan(&c.Spans, &c.ExampleTraceID); err != nil {
		return c, fmt.Errorf("clickhouse: version per run check: %w", err)
	}
	return c, nil
}

func (r *ClickHouseRepository) rejectedChecks(ctx context.Context, f MetricsFilter) ([]model.SetupCheck, error) {
	if f.UserID != "" {
		return nil, nil
	}
	scope, scopeArgs := workspaceScope(f.WorkspaceIDs)
	q := `SELECT code, toInt64(sum(spans)) FROM tracium.rejected_spans
WHERE hour >= toStartOfHour(toDateTime(?)) AND hour < toDateTime(?)` + scope + `
GROUP BY code`
	rows, err := r.db.QueryContext(ctx, q, append([]any{f.Start.Unix(), f.End.Unix()}, scopeArgs...)...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: rejected spans: %w", err)
	}
	defer rows.Close()

	var checks []model.SetupCheck
	for rows.Next() {
		var code string
		var spans int64
		if err := rows.Scan(&code, &spans); err != nil {
			return nil, fmt.Errorf("clickhouse: scan rejected spans: %w", err)
		}
		msg, ok := rejectedMessages[code]
		if !ok {
			msg = fmt.Sprintf("Spans were rejected at ingest (%s).", code)
		}
		checks = append(checks, model.SetupCheck{
			SetupIssue: model.SetupIssue{Code: "rejected_" + code, Severity: "critical", Message: msg},
			Spans:      spans,
		})
	}
	return checks, rows.Err()
}
