package genai

import "strconv"

// Token-usage and cost attribute keys. Tracium tolerates the two naming eras it
// sees in the wild: current OTel GenAI semconv (input_tokens / output_tokens,
// dotted cache keys) and OpenLLMetry's legacy prompt_tokens / completion_tokens
// (and underscore cache keys). Cost is not part of the semconv, but some
// instrumentations stamp it; we honour it when present.
const (
	attrInputTokens     = "gen_ai.usage.input_tokens"
	attrPromptTokens    = "gen_ai.usage.prompt_tokens"
	attrLLMPromptTokens = "llm.usage.prompt_tokens"

	attrOutputTokens        = "gen_ai.usage.output_tokens"
	attrCompletionTokens    = "gen_ai.usage.completion_tokens"
	attrLLMCompletionTokens = "llm.usage.completion_tokens"

	attrCacheReadDotted     = "gen_ai.usage.cache_read.input_tokens"
	attrCacheReadUnderscore = "gen_ai.usage.cache_read_input_tokens"

	attrCacheCreationDotted     = "gen_ai.usage.cache_creation.input_tokens"
	attrCacheCreationUnderscore = "gen_ai.usage.cache_creation_input_tokens"

	attrTotalTokens    = "gen_ai.usage.total_tokens"
	attrLLMTotalTokens = "llm.usage.total_tokens"

	attrCostGenAI = "gen_ai.usage.cost"
	attrCostLLM   = "llm.usage.total_cost"

	// Streaming and model markers, used to recognise a call the instrumentation
	// never metered. gen_ai.is_streaming is OpenLLMetry's OpenAI spelling,
	// llm.is_streaming its spelling for most other providers. Both are read
	// only — Tracium never writes a gen_ai.* attribute of its own.
	attrGenAIStreaming = "gen_ai.is_streaming"
	attrLLMStreaming   = "llm.is_streaming"

	attrModelRequest  = "gen_ai.request.model"
	attrModelResponse = "gen_ai.response.model"
)

// TokenUsage is the metering Tracium reads off an incoming LLM span: token
// counts across the classes providers bill separately, plus any cost the
// instrumentation already computed upstream (ReportedCostUSD > 0). It is the
// single source of truth for both the stored token columns and the pricing
// input, so the processor and exporter can't drift on which attributes count.
type TokenUsage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	ReportedCostUSD  float64

	// OutputTokensDerived reports that OutputTokens is not purely what the
	// provider broke out: part of it was reconciled from total_tokens (see
	// reconcileOutput). Callers must record it as tracium.usage.output_tokens_derived
	// so a derived count is never mistaken for a provider-reported one.
	OutputTokensDerived bool

	// Unmetered reports a span that billed real money but carries no usage at
	// all (see unmetered). Callers must record it as tracium.usage.unmetered so
	// the $0 is visibly unknown rather than silently free.
	Unmetered bool
}

// Usage extracts token metering from a span's attributes. Input and output fall
// back across the semconv and OpenLLMetry names; cache-read/creation tokens are
// read from either the dotted or underscore form. Missing attributes read as
// zero. InputTokens is passed through exactly as reported: whether it counts the
// cached tokens is provider-specific, and reconciling that is the pricing
// resolver's job, not this extractor's.
func Usage(attrs map[string]string) TokenUsage {
	u := TokenUsage{
		InputTokens:      firstInt(attrs, attrInputTokens, attrPromptTokens, attrLLMPromptTokens),
		OutputTokens:     firstInt(attrs, attrOutputTokens, attrCompletionTokens, attrLLMCompletionTokens),
		CacheReadTokens:  firstInt(attrs, attrCacheReadDotted, attrCacheReadUnderscore),
		CacheWriteTokens: firstInt(attrs, attrCacheCreationDotted, attrCacheCreationUnderscore),
		ReportedCostUSD:  firstFloat(attrs, attrCostGenAI, attrCostLLM),
	}
	u.reconcileOutput(attrs)
	u.Unmetered = unmetered(attrs, u)
	return u
}

// reconcileOutput closes the gap between the total the provider reports and the
// classes the instrumentation actually broke out, attributing the remainder to
// output. Providers bill classes they do not always surface: Google charges
// thoughts_token_count at the output rate, but OpenLLMetry maps only
// candidates_token_count onto output_tokens, so a reasoning call bills for
// tokens that otherwise never reach Tracium (Gemini 2.5 thinks by default).
//
// We reconcile against total_tokens rather than reading the provider-specific
// thinking attribute deliberately: the arithmetic is provider-agnostic, so the
// next provider that bills an unbroken-out class is covered without new code.
//
// The remainder is only claimed once every class we know of has been subtracted
// and something is still left, so a provider whose total already accounts for
// its cache tokens gets no phantom backfill. That is one-sided on purpose: the
// rule can under-count (a Gemini call that both caches and thinks reconciles
// only the excess), never over-bill.
func (u *TokenUsage) reconcileOutput(attrs map[string]string) {
	total := firstInt(attrs, attrTotalTokens, attrLLMTotalTokens)
	if total == 0 || u.InputTokens == 0 {
		return
	}
	remainder := total - u.InputTokens - u.OutputTokens - u.CacheReadTokens - u.CacheWriteTokens
	if remainder <= 0 {
		return
	}
	u.OutputTokens += remainder
	u.OutputTokensDerived = true
}

// unmetered reports a span the provider billed but the instrumentation did not
// meter: a model call that returned a completion yet carries no usage at all.
// The common cause is a streamed OpenAI call without
// stream_options={"include_usage": True} — opt-in, widely unset, and it makes
// the instrumentation emit no gen_ai.usage.* attribute whatsoever. Such a span
// still lands with full content, so the call looks tracked while costing $0
// forever.
//
// We only flag it. Estimating the tokens back from the captured content is
// lossy guesswork that would be indistinguishable from a real measurement;
// saying "usage unknown" is honest.
func unmetered(attrs map[string]string, u TokenUsage) bool {
	if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 ||
		u.CacheWriteTokens > 0 || u.ReportedCostUSD > 0 {
		return false
	}
	if !isTrue(attrs[attrGenAIStreaming]) && !isTrue(attrs[attrLLMStreaming]) {
		return false
	}
	if attrs[attrModelRequest] == "" && attrs[attrModelResponse] == "" {
		return false
	}
	// A completion proves the call did work worth billing, and distinguishes it
	// from a structural or failed span.
	return outputText(attrs) != ""
}

// isTrue reads an OTLP boolean attribute, which reaches us stringified.
func isTrue(s string) bool { return s == "true" }

// firstInt returns the first positive integer among the given keys. A zero or
// absent value falls through, so a provider that sends "0" input_tokens does not
// mask a legacy prompt_tokens value.
func firstInt(attrs map[string]string, keys ...string) int64 {
	for _, k := range keys {
		if n := parseInt(attrs[k]); n > 0 {
			return n
		}
	}
	return 0
}

// firstFloat returns the first positive float among the given keys.
func firstFloat(attrs map[string]string, keys ...string) float64 {
	for _, k := range keys {
		if f, err := strconv.ParseFloat(attrs[k], 64); err == nil && f > 0 {
			return f
		}
	}
	return 0
}

// parseInt reads a token count that may arrive as an integer ("123") or a
// float-formatted integer ("123.0"). Returns 0 for empty or unparseable input.
func parseInt(s string) int64 {
	if s == "" {
		return 0
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int64(f)
	}
	return 0
}
