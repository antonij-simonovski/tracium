package model

import "github.com/tracium/api/internal/version"

// Span represents a single OpenTelemetry span as stored and served by the API.
type Span struct {
	TraceID      string  `json:"trace_id"`
	SpanID       string  `json:"span_id"`
	ParentSpanID string  `json:"parent_span_id"`
	Name         string  `json:"name"`
	StartTimeMs  int64   `json:"start_time_ms"`
	EndTimeMs    int64   `json:"end_time_ms"`
	DurationMs   int64   `json:"duration_ms"`
	Model        string  `json:"model"`
	FinishReason string  `json:"finish_reason"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	// SubtreeCostUSD is CostUSD plus the cost of every transitive descendant
	// span, so a parent (a workflow that fans out to sub-workflows and tool calls)
	// carries the total cost of everything beneath it. Derived at read time by
	// AssignSubtreeTotals — there is no backing column, because spans stream in
	// independently and out of order, so a subtree total can only be computed
	// once the whole trace is assembled.
	SubtreeCostUSD float64 `json:"subtree_cost_usd"`
	// SubtreeInputTokens / SubtreeOutputTokens mirror SubtreeCostUSD for token
	// counts: each is the span's own token count plus that of every transitive
	// descendant, so a parent reports the total tokens consumed beneath it.
	// Derived at read time by AssignSubtreeTotals — no backing column, for the
	// same streaming reason SubtreeCostUSD has none.
	SubtreeInputTokens  int64  `json:"subtree_input_tokens"`
	SubtreeOutputTokens int64  `json:"subtree_output_tokens"`
	UserID              string `json:"user_id"`
	WorkspaceID         string `json:"workspace_id"`
	ModelNormalized     string `json:"model_normalized"`
	SchemaVersion       int    `json:"schema_version"`
	ErrorType           string `json:"error_type"`
	ErrorMessage        string `json:"error_message"`

	// Input/Output are present only when the collector has content capture enabled.
	Input          string          `json:"input,omitempty"`
	Output         string          `json:"output,omitempty"`
	AvailableTools []AvailableTool `json:"available_tools,omitempty"`

	// Kind is the normalized span role (agent, llm, tool, chain, retriever,
	// embedding), classified by the collector from instrumentation attributes.
	// Empty (omitted) when the collector could not classify the span or for
	// pre-migration rows; the dashboard then infers the role from the trace tree.
	Kind string `json:"kind,omitempty"`

	SetupIssues []SetupIssue `json:"setup_issues,omitempty"`
}

// CheckSchemaCompatibility reports whether every span can be interpreted by
// this server. A row stamped with a schema version NEWER than CurrentSchema was
// written by a newer collector during a rolling upgrade: its fields may have
// changed meaning, so it must not be served as fact — a CompatibilityError is
// returned instead. Older rows are always served: columns they predate read
// back as zero-values, which is correct, so a stale row never fails a query.
//
// Applied on the read path over one trace's spans, so it costs O(n) per
// request and nothing at scan time.
func CheckSchemaCompatibility(spans []Span) error {
	for i := range spans {
		if v := version.SchemaVersion(spans[i].SchemaVersion); !version.IsCompatible(v) {
			return &version.CompatibilityError{RowVersion: v, ServerVersion: version.CurrentSchema}
		}
	}
	return nil
}

// AssignSubtreeTotals fills SubtreeCostUSD, SubtreeInputTokens and
// SubtreeOutputTokens for every span in a single trace: each span's own value
// plus that of all its transitive descendants, so a parent reports the totals
// for everything beneath it. It runs in O(n) over the trace's spans, handles
// arbitrary nesting depth, and is safe against malformed cyclic parent links (a
// cycle simply stops contributing).
//
// Callers pass the complete set of spans for one trace; the slice is mutated in
// place. Spans whose parent is absent from the set are treated as roots.
func AssignSubtreeTotals(spans []Span) {
	children := make(map[string][]int, len(spans))
	for i := range spans {
		if p := spans[i].ParentSpanID; p != "" && p != spans[i].SpanID {
			children[p] = append(children[p], i)
		}
	}

	type totals struct {
		cost                      float64
		inputTokens, outputTokens int64
	}
	memo := make(map[string]totals, len(spans))
	visiting := make(map[string]bool, len(spans))
	var sum func(i int) totals
	sum = func(i int) totals {
		id := spans[i].SpanID
		if v, ok := memo[id]; ok {
			return v
		}
		if visiting[id] {
			return totals{} // cyclic parent link — break the loop
		}
		visiting[id] = true
		t := totals{
			cost:         spans[i].CostUSD,
			inputTokens:  spans[i].InputTokens,
			outputTokens: spans[i].OutputTokens,
		}
		for _, c := range children[id] {
			ct := sum(c)
			t.cost += ct.cost
			t.inputTokens += ct.inputTokens
			t.outputTokens += ct.outputTokens
		}
		visiting[id] = false
		memo[id] = t
		return t
	}

	for i := range spans {
		t := sum(i)
		spans[i].SubtreeCostUSD = t.cost
		spans[i].SubtreeInputTokens = t.inputTokens
		spans[i].SubtreeOutputTokens = t.outputTokens
	}
}

// AvailableTool describes a tool offered to the model on a span and whether the
// model invoked it. Decoded from the available_tools JSON column.
type AvailableTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Used        bool   `json:"used"`
}
