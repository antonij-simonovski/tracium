package genai

import "strings"

// Instrumentation attributes that carry an explicit span role, read in
// precedence order to derive the normalized Span.Kind. None are gen_ai.*
// (except the operation name) — they come from specific agent-instrumentation
// libraries that classify spans more richly than the bare token usage does.
const (
	attrOpenInferenceKind = "openinference.span.kind"
	attrTraceloopKind     = "traceloop.span.kind"
	attrGenAIOperation    = "gen_ai.operation.name"
)

// Kind derives the normalized span role from instrumentation attributes, in
// precedence order: OpenInference's explicit kind, Traceloop's decorator kind,
// the OTel GenAI operation name, and finally the presence of a model or token
// usage (which marks an LLM call). Returns "" when nothing classifies the span;
// the API omits the empty value and the dashboard infers the role from the
// span's position in the trace instead.
func Kind(attrs map[string]string, model string, inputTokens, outputTokens int64) string {
	if k := fromOpenInferenceKind(attrs[attrOpenInferenceKind]); k != "" {
		return k
	}
	if k := fromTraceloopKind(attrs[attrTraceloopKind]); k != "" {
		return k
	}
	if k := fromGenAIOperation(attrs[attrGenAIOperation]); k != "" {
		return k
	}
	if model != "" || inputTokens > 0 || outputTokens > 0 {
		return "llm"
	}
	return ""
}

// OpenInference span kinds are upper-case (LLM, TOOL, CHAIN, ...). RERANKER maps
// to retriever (a retrieval-stage operation); GUARDRAIL/EVALUATOR have no core
// equivalent and stay unclassified so the dashboard treats them as structural.
func fromOpenInferenceKind(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "llm":
		return "llm"
	case "tool":
		return "tool"
	case "chain":
		return "chain"
	case "agent":
		return "agent"
	case "retriever", "reranker":
		return "retriever"
	case "embedding":
		return "embedding"
	}
	return ""
}

// Traceloop decorator kinds: workflow/task are orchestration steps (→ chain);
// agent and tool map directly. Traceloop LLM calls carry gen_ai attributes
// rather than a traceloop.span.kind, so they fall through to the later signals.
func fromTraceloopKind(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "workflow", "task":
		return "chain"
	case "agent":
		return "agent"
	case "tool":
		return "tool"
	}
	return ""
}

// fromGenAIOperation maps the OTel GenAI gen_ai.operation.name semantic
// convention onto the normalized kinds.
func fromGenAIOperation(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "chat", "text_completion", "generate_content":
		return "llm"
	case "embeddings":
		return "embedding"
	case "execute_tool":
		return "tool"
	case "create_agent", "invoke_agent":
		return "agent"
	}
	return ""
}
