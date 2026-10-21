package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/tracium/api/internal/middleware"
	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/query"
)

// The MCP server is stateless Streamable HTTP: every POST carries one JSON-RPC
// message and gets a JSON reply, so no session or SSE stream is kept.

// 2025-03-26 is left out: it allows JSON-RPC batches, and a batch of tool calls
// would cost one request against the rate limit.
var mcpProtocolVersions = []string{"2025-11-25", "2025-06-18"}

const mcpInstructions = `Tracium holds this workspace's LLM telemetry: calls (spans) and runs (traces), with cost, tokens, latency and errors.
Every tool takes an optional workspace_id; without it, a tool reads every workspace you can read. Call list_workspaces to see them.
Call describe_schema before writing a query: it lists the views, metrics, dimensions, every stored column and the workflows, models and attribute keys present in the data.
Pass fields (and span_fields on get_trace) to return only what you need; it saves tokens.
Results are paged: when a result carries next_page (next_span_page on get_trace), pass it as page (span_page) to read more.
Use query for any question about the data: it aggregates any column (<func>:<field> metrics such as max:cost_usd, formulas, having, any-of filters), reaches a call's run and parent span (run.<field>, parent.<column>), or, with columns, lists matching calls or runs. Use list_traces to find example runs, get_trace to read one run, and detect_anomalies to explain spikes.
Cost: meta.cost_source "reconciled" matches the dashboard; "spans" counts span cost only.`

const (
	maxMCPBody        = 256 << 10
	mcpTraceListLimit = 50
	mcpSpanLimit      = 200
)

type mcpTool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations toolAnnotations `json:"annotations"`
}

// toolAnnotations tell clients how a tool behaves, so they can run read-only
// tools without asking first.
type toolAnnotations struct {
	ReadOnly    bool `json:"readOnlyHint"`
	Destructive bool `json:"destructiveHint"`
	Idempotent  bool `json:"idempotentHint"`
	OpenWorld   bool `json:"openWorldHint"`
}

// readOnly describes every Tracium tool: each only reads the caller's own data.
var readOnly = toolAnnotations{ReadOnly: true, Idempotent: true}

const workspaceProp = `"workspace_id": {"type": "string", "description": "The workspace to read, from list_workspaces. Omitted, every workspace you can read."}`

var (
	traceFields = jsonFields(reflect.TypeFor[model.Trace]())
	spanFields  = jsonFields(reflect.TypeFor[model.Span]())
)

func fieldsProp(name, what string, valid []string) string {
	return `"` + name + `": {"type": "array", "items": {"type": "string", "enum": ["` + strings.Join(valid, `", "`) + `"]}, "description": "Return only these fields of each ` + what + `. Omitted, every field."}`
}

var mcpTools = []mcpTool{
	{
		Name:        "list_workspaces",
		Title:       "List workspaces",
		Annotations: readOnly,
		Description: "Lists the workspaces you can read, with their id, name and environment. Pass an id as workspace_id to the other tools to query one workspace.",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
	},
	{
		Name:        "describe_schema",
		Title:       "Describe query schema",
		Annotations: readOnly,
		Description: "Lists the query views with their metrics, dimensions and stored columns, the metric functions and filter ops, plus the workflows, models and custom attribute keys seen in the last 30 days. Call it before writing a query.",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {` + workspaceProp + `}}`),
	},
	{
		Name:        "query",
		Title:       "Query telemetry",
		Annotations: readOnly,
		Description: "Answers questions about telemetry, aggregated or row by row. " +
			"Aggregate: pick metrics, group by dimensions, filter, and optionally bucket by time. " +
			"List: set columns instead of metrics to get the matching calls or runs themselves. " +
			"A field is a dimension, a column from describe_schema, or attr.<key>. " +
			"On the calls view (one row per LLM/tool call) run.<field> reads the call's run (e.g. run.errored, run.cost_usd) and parent.<column> its parent span (text empty and numbers 0 when there is none; filter parent_span_id not_empty to skip root calls). " +
			"On the runs view (one row per run) columns such as cost_usd, duration_ms and errored are per-run totals, so filters on them select runs. " +
			"Filters combine with AND; put alternatives in one filter's any list. formulas compute from the metrics, and having filters on metrics and formulas. " +
			"Errors name the valid choices, so fix the query and retry.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "view": {"type": "string", "enum": ["calls", "runs"], "description": "Defaults to calls."},
    "metrics": {"type": "array", "items": {"type": "string"}, "description": "Named metrics such as cost, calls, runs, total_tokens, error_rate, latency_p95_ms, or <func>:<field> with func one of sum, avg, min, max, p50, p90, p95, p99, uniq, e.g. max:cost_usd, avg:output_tokens, uniq:attr.customer, p90:run.duration_ms"},
    "dimensions": {"type": "array", "items": {"type": "string"}, "maxItems": 4, "description": "Group by fields, e.g. model, workflow, user, kind, attr.team"},
    "columns": {"type": "array", "items": {"type": "string"}, "maxItems": 30, "description": "Lists calls or runs instead of aggregating: the fields to return per row, e.g. trace_id, model, cost_usd, error_message, input, parent.name. Leave metrics, dimensions, granularity, formulas and having empty. Long text is truncated."},
    "filters": {"type": "array", "maxItems": 10, "description": "All must match.", "items": {
      "type": "object",
      "properties": {
        "field": {"type": "string", "description": "Any field of the view"},
        "op": {"type": "string", "enum": ["eq", "neq", "contains", "in", "not_in", "gt", "ge", "lt", "le", "is_empty", "not_empty"], "description": "gt, ge, lt and le need a numeric field"},
        "value": {"type": ["string", "number"], "description": "For eq, neq, contains, gt, ge, lt and le"},
        "values": {"type": "array", "items": {"type": "string"}, "description": "For in and not_in"},
        "any": {"type": "array", "maxItems": 10, "items": {"type": "object"}, "description": "Instead of field and op: filters of which at least one must match, e.g. [{\"field\": \"model\", \"op\": \"eq\", \"value\": \"gpt-4o\"}, {\"field\": \"error_type\", \"op\": \"not_empty\"}]. They do not nest."}
      }
    }},
    "formulas": {"type": "array", "maxItems": 5, "items": {
      "type": "object",
      "required": ["name", "expr"],
      "properties": {
        "name": {"type": "string", "description": "The result column, letters, digits and underscores"},
        "expr": {"type": "string", "description": "Arithmetic over this query's metrics and numbers with + - * / and parentheses, e.g. cost / runs or errors * 100 / calls. Division by zero gives 0."}
      }
    }},
    "having": {"type": "array", "maxItems": 10, "description": "Keeps aggregated rows whose metric or formula compares true; all must match.", "items": {
      "type": "object",
      "required": ["field", "op", "value"],
      "properties": {
        "field": {"type": "string", "description": "One of this query's metrics or formulas"},
        "op": {"type": "string", "enum": ["eq", "neq", "gt", "ge", "lt", "le"]},
        "value": {"type": ["string", "number"]}
      }
    }},
    "granularity": {"type": "string", "enum": ["hour", "day"], "description": "Adds a time column bucketed at this width."},
    "from": {"type": "string", "format": "date-time", "description": "RFC 3339. Defaults to 7 days before to."},
    "to": {"type": "string", "format": "date-time", "description": "RFC 3339. Defaults to now. The window is at most 92 days."},
    "order_by": {"type": "string", "description": "A metric, a formula, a dimension, or time; when listing, one of the columns. Defaults to the first metric (time with a granularity), or newest first when listing."},
    "order": {"type": "string", "enum": ["asc", "desc"]},
    "limit": {"type": "integer", "minimum": 1, "maximum": 1000, "description": "Rows per page. Defaults to 100 (50 when listing, at most 200)."},
    "page": {"type": "integer", "minimum": 1, "description": "1-indexed. Pass meta.next_page to read the next page."},
    ` + workspaceProp + `
  }
}`),
	},
	{
		Name:        "list_traces",
		Title:       "List runs",
		Annotations: readOnly,
		Description: "Lists recent runs (traces), newest first, with their workflow, cost, duration and error status. Use it to find trace ids for get_trace.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "workflow": {"type": "string"},
    "version": {"type": "string"},
    "user": {"type": "string"},
    "model": {"type": "string"},
    "has_error": {"type": "boolean"},
    "from": {"type": "string", "format": "date-time", "description": "RFC 3339. Defaults to 30 days ago."},
    "to": {"type": "string", "format": "date-time", "description": "RFC 3339. Defaults to now."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 50, "description": "Runs per page. Defaults to 20."},
    "page": {"type": "integer", "minimum": 1, "description": "1-indexed. Pass next_page to read the next page."},
    ` + fieldsProp("fields", "run", traceFields) + `,
    ` + workspaceProp + `
  }
}`),
	},
	{
		Name:        "get_trace",
		Title:       "Read a run",
		Annotations: readOnly,
		Description: "Reads one run: its summary and its spans (model, tokens, cost, timing, errors), 200 spans per page. Long span input/output is truncated.",
		InputSchema: json.RawMessage(`{"type": "object", "required": ["trace_id"], "properties": {"trace_id": {"type": "string"}, ` +
			fieldsProp("fields", "run summary", traceFields) + `, ` + fieldsProp("span_fields", "span", spanFields) +
			`, "span_page": {"type": "integer", "minimum": 1, "description": "1-indexed. Pass next_span_page to read more spans."}, ` + workspaceProp + `}}`),
	},
	{
		Name:        "detect_anomalies",
		Title:       "Detect anomalies",
		Annotations: readOnly,
		Description: "Finds statistically significant daily changes in cost, error rate or run volume, workspace-wide and per busy workflow, with the workflow versions that shipped around them.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "range": {"type": "string", "enum": ["7d", "30d", "90d"], "description": "Defaults to 30d."},
    "metric": {"type": "string", "enum": ["cost", "error_rate", "runs"]},
    "min_severity": {"type": "string", "enum": ["info", "warning", "critical"]},
    ` + workspaceProp + `
  }
}`),
	},
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// toolError is a failure the model should see and recover from, reported as
// an isError tool result rather than a JSON-RPC error.
type toolError struct{ msg string }

func (e toolError) Error() string { return e.msg }

// MCP handles POST /v1/mcp.
func (h *QueryHandler) MCP(w http.ResponseWriter, r *http.Request) {
	// A browser page on another site must not reach the server, even through a
	// DNS-rebound name; non-browser clients send no Origin.
	if origin := r.Header.Get("Origin"); origin != "" {
		if u, err := url.Parse(origin); err != nil || u.Host != r.Host {
			respondError(w, http.StatusForbidden, "FORBIDDEN", "origin not allowed")
			return
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxMCPBody)
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			respondDecodeError(w, err)
			return
		}
		respondJSON(w, http.StatusBadRequest, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
		return
	}
	// Notifications and replies to server requests carry no id to answer.
	if len(req.ID) == 0 || req.Method == "" {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	result, rerr := h.dispatch(r.Context(), req)
	if rerr != nil {
		resp.Error = rerr
	} else {
		resp.Result = result
	}
	respondJSON(w, http.StatusOK, resp)
}

func (h *QueryHandler) dispatch(ctx context.Context, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := mcpProtocolVersions[0]
		for _, v := range mcpProtocolVersions {
			if v == p.ProtocolVersion {
				version = v
			}
		}
		return map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "tracium", "version": "1"},
			"instructions":    mcpInstructions,
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": mcpTools}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{-32602, "invalid params"}
		}
		if len(p.Arguments) == 0 || string(p.Arguments) == "null" {
			p.Arguments = json.RawMessage("{}")
		}
		out, err := h.callTool(ctx, p.Name, p.Arguments)
		var te toolError
		if errors.As(err, &te) {
			return map[string]any{"content": []map[string]any{{"type": "text", "text": te.msg}}, "isError": true}, nil
		}
		if errors.Is(err, errUnknownTool) {
			return nil, &rpcError{-32602, "unknown tool " + p.Name}
		}
		if err != nil {
			log.Printf("mcp %s: %v", p.Name, err)
			return map[string]any{"content": []map[string]any{{"type": "text", "text": "the query failed; try a narrower window"}}, "isError": true}, nil
		}
		// The JSON goes out once, as text: no outputSchema is declared, and a
		// structuredContent copy would double what the client may read.
		text, _ := json.Marshal(out)
		return map[string]any{"content": []map[string]any{{"type": "text", "text": string(text)}}}, nil
	}
	return nil, &rpcError{-32601, "method not found"}
}

var errUnknownTool = errors.New("unknown tool")

type scopedArgs struct {
	WorkspaceID string `json:"workspace_id"`
}

func (h *QueryHandler) callTool(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	var scoped scopedArgs
	if err := json.Unmarshal(raw, &scoped); err != nil {
		return nil, toolError{"arguments must be a JSON object"}
	}
	scope, serr := workspaceScope(ctx, h.access, scoped.WorkspaceID)
	if serr != nil {
		return nil, toolError{serr.message}
	}

	switch name {
	case "list_workspaces":
		return h.listWorkspaces(ctx, scope)
	case "describe_schema":
		return h.schema(ctx, scope)
	case "query":
		var q query.Query
		if err := json.Unmarshal(raw, &q); err != nil {
			return nil, toolError{"invalid arguments: " + err.Error()}
		}
		res, err := h.run(ctx, scope, q)
		if errors.Is(err, query.ErrInvalidQuery) {
			return nil, toolError{err.Error()}
		}
		return res, err
	case "list_traces":
		return h.listTraces(ctx, scope, raw)
	case "get_trace":
		return h.getTrace(ctx, scope, raw)
	case "detect_anomalies":
		return h.anomalies(ctx, scope, raw)
	}
	return nil, errUnknownTool
}

func (h *QueryHandler) listWorkspaces(ctx context.Context, scope []string) (any, error) {
	principal, _ := middleware.PrincipalFromContext(ctx)
	all, err := h.access.List(ctx, principal.UserID)
	if err != nil {
		return nil, err
	}
	type entry struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Env  string `json:"env"`
	}
	out := []entry{}
	for _, ws := range all {
		if slices.Contains(scope, ws.ID) {
			out = append(out, entry{ws.ID, ws.Name, ws.Env})
		}
	}
	return map[string]any{"workspaces": out}, nil
}

func (h *QueryHandler) listTraces(ctx context.Context, scope []string, raw json.RawMessage) (any, error) {
	var a struct {
		Workflow string    `json:"workflow"`
		Version  string    `json:"version"`
		User     string    `json:"user"`
		Model    string    `json:"model"`
		HasError *bool     `json:"has_error"`
		From     time.Time `json:"from"`
		To       time.Time `json:"to"`
		Limit    int       `json:"limit"`
		Page     int       `json:"page"`
		Fields   []string  `json:"fields"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, toolError{"invalid arguments: " + err.Error()}
	}
	if a.Limit <= 0 {
		a.Limit = 20
	}
	if a.Limit > mcpTraceListLimit {
		return nil, toolError{fmt.Sprintf("limit must be at most %d", mcpTraceListLimit)}
	}
	f := query.TraceFilter{
		UserID: a.User, WorkspaceIDs: scope, Model: a.Model, Workflow: a.Workflow, Version: a.Version,
		HasError: a.HasError, StartAfter: a.From, StartBefore: a.To, PageSize: a.Limit, Page: a.Page,
	}
	if err := f.Validate(); err != nil {
		return nil, toolError{err.Error()}
	}
	traces, total, err := h.traces.ListTraces(ctx, f)
	if err != nil {
		return nil, err
	}
	picked, err := pick(traces, a.Fields, traceFields)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"traces": picked, "total": total, "page": f.Page}
	if int64(f.Page*f.PageSize) < total {
		out["next_page"] = f.Page + 1
	}
	return out, nil
}

func (h *QueryHandler) getTrace(ctx context.Context, scope []string, raw json.RawMessage) (any, error) {
	var a struct {
		TraceID    string   `json:"trace_id"`
		Fields     []string `json:"fields"`
		SpanFields []string `json:"span_fields"`
		SpanPage   int      `json:"span_page"`
	}
	if err := json.Unmarshal(raw, &a); err != nil || a.TraceID == "" {
		return nil, toolError{"trace_id is required"}
	}
	trace, err := h.traces.GetTrace(ctx, a.TraceID, scope)
	if errors.Is(err, query.ErrNotFound) {
		return nil, toolError{"trace not found"}
	}
	if err != nil {
		return nil, err
	}
	spans, err := h.traces.GetSpans(ctx, a.TraceID, scope)
	if err != nil {
		return nil, err
	}
	a.SpanPage = max(a.SpanPage, 1)
	total, lo := len(spans), len(spans)
	if a.SpanPage <= total/mcpSpanLimit+1 {
		lo = min((a.SpanPage-1)*mcpSpanLimit, total)
	}
	hi := min(lo+mcpSpanLimit, total)
	spans = spans[lo:hi]
	for i := range spans {
		spans[i].Input = query.ClipText(spans[i].Input)
		spans[i].Output = query.ClipText(spans[i].Output)
	}
	pickedTrace, err := pick([]model.Trace{*trace}, a.Fields, traceFields)
	if err != nil {
		return nil, err
	}
	pickedSpans, err := pick(spans, a.SpanFields, spanFields)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"trace": pickedTrace[0], "spans": pickedSpans, "total_spans": total, "span_page": a.SpanPage}
	if hi < total {
		out["next_span_page"] = a.SpanPage + 1
	}
	return out, nil
}

// jsonFields lists t's JSON keys, so a field added to the model is selectable
// with no change here.
func jsonFields(t reflect.Type) []string {
	var out []string
	for _, f := range reflect.VisibleFields(t) {
		if name, _, _ := strings.Cut(f.Tag.Get("json"), ","); f.IsExported() && !f.Anonymous && name != "" && name != "-" {
			out = append(out, name)
		}
	}
	return out
}

// pick keeps only fields of each item; no fields keeps everything.
func pick[T any](items []T, fields, valid []string) ([]any, error) {
	for _, f := range fields {
		if !slices.Contains(valid, f) {
			return nil, toolError{fmt.Sprintf("unknown field %q; valid: %s", f, strings.Join(valid, ", "))}
		}
	}
	out := make([]any, len(items))
	for i, item := range items {
		if len(fields) == 0 {
			out[i] = item
			continue
		}
		raw, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		var all map[string]any
		if err := json.Unmarshal(raw, &all); err != nil {
			return nil, err
		}
		kept := make(map[string]any, len(fields))
		for _, f := range fields {
			if v, ok := all[f]; ok {
				kept[f] = v
			}
		}
		out[i] = kept
	}
	return out, nil
}

func (h *QueryHandler) anomalies(ctx context.Context, scope []string, raw json.RawMessage) (any, error) {
	var a struct {
		Range       string `json:"range"`
		Metric      string `json:"metric"`
		MinSeverity string `json:"min_severity"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, toolError{"invalid arguments: " + err.Error()}
	}
	if a.Range == "" {
		a.Range = "30d"
	}
	if a.Range != "7d" && a.Range != "30d" && a.Range != "90d" {
		return nil, toolError{"range must be one of 7d, 30d, 90d"}
	}
	if a.Metric != "" && !validAnomalyMetrics[a.Metric] {
		return nil, toolError{"metric must be one of cost, error_rate, runs"}
	}
	if a.MinSeverity != "" && !validAnomalySeverities[a.MinSeverity] {
		return nil, toolError{"min_severity must be one of info, warning, critical"}
	}
	f, _ := query.ParseRange(a.Range, h.now())
	f.WorkspaceIDs = scope
	f.AnomalyMetric, f.AnomalyMinSeverity = a.Metric, a.MinSeverity
	found, err := h.metrics.Anomalies(ctx, f)
	if err != nil {
		return nil, err
	}
	if found == nil {
		found = []model.Anomaly{}
	}
	return map[string]any{"anomalies": found}, nil
}
