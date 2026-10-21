package genai

import "testing"

func TestUsage_SemconvNames(t *testing.T) {
	u := Usage(map[string]string{
		attrInputTokens:  "1200",
		attrOutputTokens: "300",
	})
	if u.InputTokens != 1200 || u.OutputTokens != 300 {
		t.Errorf("got %+v", u)
	}
}

// OpenLLMetry (Traceloop) still emits the legacy prompt_tokens/completion_tokens
// names widely. These must be read too, or such spans price at zero.
func TestUsage_OpenLLMetryLegacyNames(t *testing.T) {
	u := Usage(map[string]string{
		attrPromptTokens:     "800",
		attrCompletionTokens: "150",
	})
	if u.InputTokens != 800 || u.OutputTokens != 150 {
		t.Errorf("legacy names not read: %+v", u)
	}
}

// A zero on the preferred key must not mask a value on a fallback key.
func TestUsage_ZeroFallsThroughToLegacy(t *testing.T) {
	u := Usage(map[string]string{
		attrInputTokens:  "0",
		attrPromptTokens: "500",
	})
	if u.InputTokens != 500 {
		t.Errorf("InputTokens = %d, want 500", u.InputTokens)
	}
}

func TestUsage_CacheTokensBothForms(t *testing.T) {
	dotted := Usage(map[string]string{attrCacheReadDotted: "700", attrCacheCreationDotted: "40"})
	if dotted.CacheReadTokens != 700 || dotted.CacheWriteTokens != 40 {
		t.Errorf("dotted cache keys: %+v", dotted)
	}
	under := Usage(map[string]string{attrCacheReadUnderscore: "700", attrCacheCreationUnderscore: "40"})
	if under.CacheReadTokens != 700 || under.CacheWriteTokens != 40 {
		t.Errorf("underscore cache keys: %+v", under)
	}
}

func TestUsage_FloatFormattedTokenCount(t *testing.T) {
	if got := Usage(map[string]string{attrInputTokens: "1234.0"}).InputTokens; got != 1234 {
		t.Errorf("InputTokens = %d, want 1234", got)
	}
}

// The Gemini thinking case, with the counts measured on a real 2.5-flash call:
// Google billed 39 input + 213 visible + 582 thinking, but the instrumentation
// reports only the visible 213 as output. total_tokens is the tell.
func TestUsage_TotalTokensReconcilesUnreportedOutput(t *testing.T) {
	u := Usage(map[string]string{
		attrInputTokens:  "39",
		attrOutputTokens: "213",
		attrTotalTokens:  "834",
	})
	if u.OutputTokens != 795 {
		t.Errorf("OutputTokens = %d, want 795 (213 visible + 582 thinking)", u.OutputTokens)
	}
	if !u.OutputTokensDerived {
		t.Error("a reconciled output count must be marked derived")
	}
}

// A total that already reconciles must be left alone, in either cache
// convention: input-inclusive (OpenAI, Gemini) or input-exclusive (Anthropic).
func TestUsage_ReconciledTotalIsNotBackfilled(t *testing.T) {
	tests := map[string]map[string]string{
		"no cache": {attrInputTokens: "100", attrOutputTokens: "50", attrTotalTokens: "150"},
		"cache inside input": {
			attrInputTokens: "100", attrOutputTokens: "50",
			attrCacheReadDotted: "80", attrTotalTokens: "150",
		},
		"cache beside input": {
			attrInputTokens: "20", attrOutputTokens: "50",
			attrCacheReadDotted: "80", attrCacheCreationDotted: "10", attrTotalTokens: "160",
		},
	}
	for name, attrs := range tests {
		t.Run(name, func(t *testing.T) {
			u := Usage(attrs)
			if u.OutputTokens != 50 || u.OutputTokensDerived {
				t.Errorf("output was backfilled from a reconciling total: %+v", u)
			}
		})
	}
}

// Without a reported total there is nothing to reconcile against, and a total
// alone must never conjure usage out of an unmetered span.
func TestUsage_ReconcileNeedsBothSignals(t *testing.T) {
	if u := Usage(map[string]string{attrInputTokens: "39", attrOutputTokens: "213"}); u.OutputTokens != 213 {
		t.Errorf("OutputTokens = %d, want 213 (no total to reconcile)", u.OutputTokens)
	}
	if u := Usage(map[string]string{attrTotalTokens: "834"}); u.OutputTokens != 0 {
		t.Errorf("OutputTokens = %d, want 0 (total alone is not a breakdown)", u.OutputTokens)
	}
}

// A streamed call whose completion landed but whose usage did not is the single
// most likely cause of silent under-reporting: without include_usage the OpenAI
// instrumentation emits no gen_ai.usage.* attribute at all.
func TestUsage_StreamedCallWithoutUsageIsFlagged(t *testing.T) {
	u := Usage(map[string]string{
		attrModelRequest:               "gpt-4o-mini",
		attrGenAIStreaming:             "true",
		prefixCompletion + "0.content": "one, two, three",
		prefixCompletion + "0.role":    "assistant",
	})
	if !u.Unmetered {
		t.Errorf("streamed call with a completion and no usage not flagged: %+v", u)
	}
	// The legacy spelling other providers use must be read too.
	legacy := Usage(map[string]string{
		attrModelResponse:              "claude-haiku-4-5",
		attrLLMStreaming:               "true",
		prefixCompletion + "0.content": "hi",
	})
	if !legacy.Unmetered {
		t.Errorf("llm.is_streaming not honoured: %+v", legacy)
	}
}

func TestUsage_MeteredOrNonStreamingSpansAreNotFlagged(t *testing.T) {
	tests := map[string]map[string]string{
		"streamed but metered": {
			attrModelRequest: "gpt-4o-mini", attrGenAIStreaming: "true",
			attrInputTokens: "12", attrOutputTokens: "8",
			prefixCompletion + "0.content": "one, two, three",
		},
		"not streamed": {
			attrModelRequest:               "gpt-4o-mini",
			prefixCompletion + "0.content": "one, two, three",
		},
		"no completion": {
			attrModelRequest: "gpt-4o-mini", attrGenAIStreaming: "true",
		},
		"structural span": {
			attrGenAIStreaming:             "true",
			prefixCompletion + "0.content": "one, two, three",
		},
	}
	for name, attrs := range tests {
		t.Run(name, func(t *testing.T) {
			if Usage(attrs).Unmetered {
				t.Error("span wrongly flagged unmetered")
			}
		})
	}
}
