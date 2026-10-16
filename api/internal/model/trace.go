package model

// Trace represents a summarised view of a distributed trace.
type Trace struct {
	TraceID         string  `json:"trace_id"`
	Name            string  `json:"name"`
	WorkflowVersion string  `json:"workflow_version"`
	StartTimeMs     int64   `json:"start_time_ms"`
	EndTimeMs       int64   `json:"end_time_ms"`
	DurationMs      int64   `json:"duration_ms"`
	UserID          string  `json:"user_id"`
	WorkspaceID     string  `json:"workspace_id"`
	SpanCount       int     `json:"span_count"`
	HasError        bool    `json:"has_error"`
	TotalCostUSD    float64 `json:"total_cost_usd"`
}

// TraceDetail extends Trace with the full list of constituent spans.
type TraceDetail struct {
	Trace
	Spans []Span `json:"spans"`
}
