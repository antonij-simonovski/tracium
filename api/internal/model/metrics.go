package model

// Metrics power the dashboard overview. All values are raw numbers; the
// dashboard owns formatting (currency, percentages, seconds).

// Anomaly is one bucket of one series that deviated significantly from its own
// recent history — the output of statistical anomaly detection. It is daily,
// rollup-backed, and workspace-scoped like every other metric. Metric is
// "cost" | "error_rate" | "runs"; Scope is "workspace" (the whole window) or
// "workflow" (Workflow names which one). Direction is "spike" | "drop"; Severity is
// "info" | "warning" | "critical". Observed is the bucket's value, Expected the
// baseline median, Deviation their difference, and Score the signed robust
// z-score behind the ranking. Summary is a ready-to-read sentence; the dashboard
// may reformat from the raw fields instead.
type Anomaly struct {
	Metric    string  `json:"metric"`
	Scope     string  `json:"scope"`
	Workflow  string  `json:"workflow"` // set when Scope == "workflow", else ""
	BucketMs  int64   `json:"bucket_ms"`
	Observed  float64 `json:"observed"`
	Expected  float64 `json:"expected"`
	Deviation float64 `json:"deviation"`
	Score     float64 `json:"score"`
	Direction string  `json:"direction"`
	Severity  string  `json:"severity"`
	Summary   string  `json:"summary"`
}

// KPI is a single headline metric with its period-over-period change.
// Delta is a fraction (0.12 == +12%); DeltaType is "good" | "bad" | "neutral".
type KPI struct {
	Value     float64 `json:"value"`
	Delta     float64 `json:"delta"`
	DeltaType string  `json:"delta_type"`
}

// KPISet is the four headline metrics shown across the top of the overview.
type KPISet struct {
	Cost       KPI `json:"cost"`        // total cost, USD
	Runs       KPI `json:"runs"`        // workflow runs (distinct traces)
	LatencyP95 KPI `json:"latency_p95"` // p95 trace duration, ms
	ErrorRate  KPI `json:"error_rate"`  // fraction of traces with an error
}

// CostPoint is one time bucket of total cost.
type CostPoint struct {
	BucketMs int64   `json:"bucket_ms"`
	Value    float64 `json:"value"`
}

// LatencyPoint is one time bucket of latency percentiles, in ms. The percentiles
// are pointers so a bucket with no runs serialises them as null rather than 0:
// latency is undefined when nothing ran, and a literal 0 would draw a misleading
// dip to the floor. The series is null-filled (not zero-filled like cost/error)
// over the full window so the x-axis still lines up with the cost chart.
type LatencyPoint struct {
	BucketMs int64    `json:"bucket_ms"`
	P50      *float64 `json:"p50"`
	P95      *float64 `json:"p95"`
	P99      *float64 `json:"p99"`
}

// WorkflowCost is one workflow's spend over the window. Trend is the workflow's call
// count per time bucket across the window (oldest first, zero-filled), powering
// the per-row usage sparkline.
type WorkflowCost struct {
	Name  string  `json:"name"`
	Cost  float64 `json:"cost"`
	Calls int64   `json:"calls"`
	Trend []int64 `json:"trend"`
}

// Workflow is one workflow's activity over the window, powering the Workflows page table.
// It is the operational companion to WorkflowCost — same workflow derivation
// (workflowExpr) and call-count Trend sparkline — extended with the latency and
// reliability columns the table shows. AvgLatencyMs is mean end-to-end run
// duration; over a long window served from the daily rollup it is 0 (per-trace
// durations aren't retained — same limitation as LatencyP95). ErrorRate is the
// fraction of the workflow's runs that errored.
type Workflow struct {
	Name         string  `json:"name"`
	Calls        int64   `json:"calls"`
	Cost         float64 `json:"cost"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	ErrorRate    float64 `json:"error_rate"`
	Trend        []int64 `json:"trend"`
	// LastTraceID is the workflow's most recent trace in the window, so the UI can
	// deep-link a row straight to that trace's detail. Empty for long windows
	// served from the daily rollup, which doesn't retain trace identity.
	LastTraceID string `json:"last_trace_id"`
}

// ErrorPoint is one time bucket of run reliability: how many runs errored out
// of the total runs that started in the bucket. The dashboard renders these as
// the "Failures by day" horizon strip.
type ErrorPoint struct {
	BucketMs int64 `json:"bucket_ms"`
	Errors   int64 `json:"errors"`
	Total    int64 `json:"total"`
}

// TokenPoint is one time bucket of token usage, split into prompt (input) and
// completion (output) tokens. Span-sourced, like the model-costs token totals.
type TokenPoint struct {
	BucketMs     int64 `json:"bucket_ms"`
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// WorkflowDetail is one workflow's detail-page payload: its headline metrics over the
// window plus the version and tool surface of its most recent run. Span-backed
// only — there is no workflow-config store, so runtime params (temperature,
// retries, owner, …) are not served. P95LatencyMs is a pointer so it serialises
// as null when undefined (no runs in the window). It is a raw-window payload
// (≤30d): the handler rejects rollup ranges, since per-workflow latency can't be
// derived from the daily rollup (same limitation as LatencySeries).
type WorkflowDetail struct {
	Name           string          `json:"name"`
	Calls          int64           `json:"calls"`
	Cost           float64         `json:"cost"`
	AvgLatencyMs   float64         `json:"avg_latency_ms"`
	P95LatencyMs   *float64        `json:"p95_latency_ms"`
	ErrorRate      float64         `json:"error_rate"`
	InputTokens    int64           `json:"input_tokens"`
	OutputTokens   int64           `json:"output_tokens"`
	Model          string          `json:"model"`    // modal model_normalized over the window
	Provider       string          `json:"provider"` // inferred from the model id; "" when unrecognised
	Tools          []AvailableTool `json:"tools"`    // union of available_tools from the latest run
	LastTraceID    string          `json:"last_trace_id"`
	CurrentVersion string          `json:"current_version"`
}

// WorkflowVersion is one version of a workflow over the window; P95LatencyMs is nil over rollup ranges.
type WorkflowVersion struct {
	Version      string   `json:"version"`
	FirstSeenMs  int64    `json:"first_seen_ms"`
	LastSeenMs   int64    `json:"last_seen_ms"`
	Calls        int64    `json:"calls"`
	Cost         float64  `json:"cost"`
	CostPerRun   float64  `json:"cost_per_run"`
	P95LatencyMs *float64 `json:"p95_latency_ms"`
	ErrorRate    float64  `json:"error_rate"`
	TokensPerRun float64  `json:"tokens_per_run"`
}

// VersionComparison holds each KPI as the candidate's value and its change from the base.
type VersionComparison struct {
	Base         WorkflowVersion `json:"base"`
	Candidate    WorkflowVersion `json:"candidate"`
	CostPerRun   KPI             `json:"cost_per_run"`
	P95LatencyMs KPI             `json:"p95_latency_ms"`
	ErrorRate    KPI             `json:"error_rate"`
	TokensPerRun KPI             `json:"tokens_per_run"`
}

// Failure aggregates errored runs for one workflow. Pct is the fraction of that
// workflow's runs that failed.
type Failure struct {
	Workflow string  `json:"workflow"`
	Count    int64   `json:"count"`
	Pct      float64 `json:"pct"`
	TopError string  `json:"top_error"`
}

// ModelCost is one model's spend over the window, powering the usage page's
// "Where it goes" list. Calls is the run (trace) count attributed to the model;
// InputTokens/OutputTokens are summed across its spans.
type ModelCost struct {
	Name         string  `json:"name"`
	Cost         float64 `json:"cost"`
	Calls        int64   `json:"calls"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
}

// AttributeUsage is spend/usage grouped by one value of a custom attribute —
// the allocation primitive that splits AI cost across whatever dimension the
// instrumentation tags spans with (team, user, environment, customer, …).
// Value is the attribute's value; the rest are its totals over the window.
type AttributeUsage struct {
	Value        string  `json:"value"`
	Cost         float64 `json:"cost"`
	Calls        int64   `json:"calls"`
	Runs         int64   `json:"runs"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
}

// UserUsage is one user's spend over the current window paired with the
// equal-length preceding window, so the usage table can show per-column change.
// Trend is cost per time bucket across the current window (oldest first,
// zero-filled) for the row sparkline.
type UserUsage struct {
	UserID   string    `json:"user_id"`
	Cost     float64   `json:"cost"`
	CostPrev float64   `json:"cost_prev"`
	Runs     int64     `json:"runs"`
	RunsPrev int64     `json:"runs_prev"`
	Trend    []float64 `json:"trend"`
}

// WorkflowUsage is one workflow's spend over the current window paired with the
// preceding window (see UserUsage). Model is the workflow's most-used model.
type WorkflowUsage struct {
	Name     string  `json:"name"`
	Model    string  `json:"model"`
	Cost     float64 `json:"cost"`
	CostPrev float64 `json:"cost_prev"`
	Runs     int64   `json:"runs"`
	RunsPrev int64   `json:"runs_prev"`
}
