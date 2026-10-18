package spanmodel

// Span is the canonical in-memory representation of an LLM trace span.
type Span struct {
	TraceID      string
	SpanID       string
	ParentSpanID string
	Name         string
	StartTimeMs  int64
	EndTimeMs    int64
	DurationMs   int64

	Model        string
	InputTokens  int64
	OutputTokens int64
	FinishReason string

	// CacheReadTokens and CacheWriteTokens are used only for pricing and are not
	// stored. Whether InputTokens includes them is provider-specific (see pricing.Usage).
	CacheReadTokens  int64
	CacheWriteTokens int64

	// ReportedCostUSD is the client-reported cost, ignored unless
	// PricingEnricher.TrustReportedCost is set.
	ReportedCostUSD float64

	CostUSD float64
	UserID  string
	// WorkspaceID is the isolation boundary, set from the verified ingest key's
	// workspace; any sender-supplied tracium.workspace.id is overwritten.
	WorkspaceID     string
	ModelNormalized string
	SchemaVersion   int
	ErrorType       string
	ErrorMessage    string

	// WorkflowName is the first non-empty of gen_ai.agent.name,
	// traceloop.workflow.name, traceloop.entity.name, service.name, or the span name.
	WorkflowName string

	// ServiceName is the resource service.name; "unknown_service*" is stored as empty.
	ServiceName string

	// WorkflowVersion is the tracium.workflow.version the span ran under.
	WorkflowVersion string

	// Source is "span" for a per-call span or "metric" for a row synthesised from
	// an OTLP token-usage metric. Empty is persisted as "span".
	Source string

	// Input and Output are populated only when content capture is enabled.
	// AvailableTools is the JSON-encoded tracium.available_tools attribute.
	Input          string
	Output         string
	AvailableTools string

	// OutputTokensDerived marks OutputTokens as partly reconciled from
	// total_tokens rather than reported whole by the provider.
	OutputTokensDerived bool

	// Unmetered marks a span that returned output but carried no usage, so its
	// zero cost is unknown rather than free.
	Unmetered bool

	// Kind is the normalized span role (agent, llm, tool, chain, retriever,
	// embedding), or empty when unclassified.
	Kind string

	// Attributes holds the custom resource and span attributes not promoted to a
	// typed field (gen_ai.*, tracium.*, llm.*, traceloop.* and service.name are excluded).
	Attributes map[string]string
}
