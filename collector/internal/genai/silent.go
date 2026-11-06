package genai

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Silent-failure reasons: a span that reported success but went wrong.
const (
	SilentTruncated = "truncated"
	SilentRefused   = "refused"
	SilentToolError = "tool_error"
)

const (
	attrToolCallResult   = "gen_ai.tool.call.result"
	attrOpenInferenceOut = "output.value"

	refusalWindow  = 160
	maxToolPayload = 64 << 10
)

var truncationReasons = map[string]bool{
	"length": true, "max_tokens": true, "max_output_tokens": true, "model_length": true,
}

var filterReasons = map[string]bool{
	"content_filter": true, "content_filtered": true, "refusal": true, "safety": true,
	"recitation": true, "blocklist": true, "prohibited_content": true, "spii": true,
	"image_safety": true, "guardrail_intervened": true,
}

var refusalPattern = regexp.MustCompile(`^(?:` +
	`(?:i'm sorry|i am sorry|sorry|i apologi[sz]e)[,.!]?\s+(?:but\s+)?i\s+(?:can't|cannot|can not|won't|am unable|'m unable|am not able|'m not able)` +
	`|i\s+(?:can't|cannot|can not|won't|am unable to|'m unable to|am not able to|'m not able to)\s+(?:help|assist|comply|provide|fulfill|fulfil|answer|do that|support)` +
	`|i must decline` +
	`)`)

// SilentFailure classifies a span that did not error but still failed: cut off
// by the token limit, refused or filtered, or a tool that returned an error
// payload. Returns "" for a healthy span.
func SilentFailure(attrs map[string]string, kind, finishReason string, errored bool) string {
	if errored {
		return ""
	}
	reason := normalizeReason(finishReason)
	switch {
	case filterReasons[reason]:
		return SilentRefused
	case truncationReasons[reason]:
		return SilentTruncated
	}
	switch kind {
	case "llm":
		if isRefusal(outputText(attrs)) {
			return SilentRefused
		}
	case "tool":
		if isErrorPayload(toolOutput(attrs)) {
			return SilentToolError
		}
	}
	return ""
}

func normalizeReason(r string) string {
	r = strings.ToLower(strings.TrimSpace(r))
	r = strings.TrimPrefix(r, "finish_reason_")
	return strings.NewReplacer("-", "_", " ", "_").Replace(r)
}

func isRefusal(out string) bool {
	out = strings.TrimSpace(out)
	out = strings.TrimSpace(strings.TrimPrefix(out, "assistant:"))
	if len(out) > refusalWindow {
		out = out[:refusalWindow]
	}
	out = strings.ToLower(strings.ReplaceAll(out, "’", "'"))
	return refusalPattern.MatchString(out)
}

func toolOutput(attrs map[string]string) string {
	if out := outputText(attrs); out != "" {
		return out
	}
	if out := attrs[attrToolCallResult]; out != "" {
		return out
	}
	return attrs[attrOpenInferenceOut]
}

// isErrorPayload accepts only a JSON object whose top-level keys say it failed;
// prose that mentions an error never matches.
func isErrorPayload(out string) bool {
	out = strings.TrimSpace(out)
	if len(out) > maxToolPayload || !strings.HasPrefix(out, "{") {
		return false
	}
	var obj map[string]any
	if json.Unmarshal([]byte(out), &obj) != nil {
		return false
	}
	for _, k := range []string{"error", "exception", "errors"} {
		if present(obj[k]) {
			return true
		}
	}
	for _, k := range []string{"status", "status_code", "statusCode"} {
		switch v := obj[k].(type) {
		case float64:
			if v >= 400 && v < 600 {
				return true
			}
		case string:
			if strings.EqualFold(v, "error") {
				return true
			}
		}
	}
	return false
}

func present(v any) bool {
	switch v := v.(type) {
	case nil:
		return false
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		return strings.TrimSpace(v) != ""
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	}
	return true
}
