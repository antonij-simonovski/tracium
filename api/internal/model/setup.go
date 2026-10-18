package model

// SetupIssue is a problem with how telemetry is instrumented, and its fix.
type SetupIssue struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// SetupCheck is a SetupIssue found in a window, with the spans it affected.
type SetupCheck struct {
	SetupIssue
	Spans          int64   `json:"spans"`
	Share          float64 `json:"share"`
	ExampleTraceID string  `json:"example_trace_id"`
}
