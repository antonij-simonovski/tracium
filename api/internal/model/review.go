package model

import "time"

const (
	VerdictCorrect   = "correct"
	VerdictIncorrect = "incorrect"
	// VerdictMissing means the run or span gave no usable output.
	VerdictMissing = "missing"

	ReviewSourceUser = "user"
	ReviewSourceAPI  = "api"
)

// Review is a verdict on one run, or on one span when SpanID is set, optionally
// with the corrected output. Source is "api" when an ingest key sent it.
type Review struct {
	TraceID         string        `json:"trace_id"`
	SpanID          string        `json:"span_id"`
	SpanName        string        `json:"span_name"`
	WorkspaceID     string        `json:"workspace_id"`
	WorkflowName    string        `json:"workflow_name"`
	WorkflowVersion string        `json:"workflow_version"`
	Verdict         string        `json:"verdict"`
	Output          string        `json:"output"`
	CorrectedOutput *string       `json:"corrected_output"`
	Note            string        `json:"note"`
	ReviewedBy      string        `json:"reviewed_by"`
	Source          string        `json:"source"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
	Fields          []ReviewField `json:"fields"`
}

// ReviewField is one JSON field of a reviewed output. Output and Corrected are
// the raw JSON values; empty means the field is absent on that side. Missing
// is set when the output left out a field the correction has.
type ReviewField struct {
	Path      string `json:"path"`
	Output    string `json:"output"`
	Corrected string `json:"corrected"`
	Correct   bool   `json:"correct"`
	Missing   bool   `json:"missing"`
}

// VersionAccuracy is the share of reviewed runs of one version marked correct.
type VersionAccuracy struct {
	// Span is the span name, "" for the run itself.
	Span         string  `json:"span"`
	Version      string  `json:"version"`
	ReviewedRuns int64   `json:"reviewed_runs"`
	CorrectRuns  int64   `json:"correct_runs"`
	Accuracy     float64 `json:"accuracy"`
}

// ReviewedSpan is a span name with reviews, and how many.
type ReviewedSpan struct {
	Name     string `json:"name"`
	Reviewed int64  `json:"reviewed"`
}

// FieldAccuracy is the share of reviewed runs in which one field was correct.
type FieldAccuracy struct {
	Path     string  `json:"path"`
	Reviewed int64   `json:"reviewed"`
	Wrong    int64   `json:"wrong"`
	Missing  int64   `json:"missing"`
	Accuracy float64 `json:"accuracy"`
}
