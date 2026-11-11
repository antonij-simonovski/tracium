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
	attrOpenInferenceMsg = "llm.output_messages.0.message.content"
	attrFinishReasons    = "gen_ai.response.finish_reasons"

	refusalWindow   = 160
	maxRefusalChars = 600
	maxToolPayload  = 64 << 10
)

var truncationReasons = map[string]bool{
	"length": true, "max_tokens": true, "max_output_tokens": true, "model_length": true,
	"incomplete": true,
}

var filterReasons = map[string]bool{
	"content_filter": true, "content_filtered": true, "refusal": true, "safety": true,
	"recitation": true, "blocklist": true, "prohibited_content": true, "spii": true,
	"image_safety": true, "guardrail_intervened": true,
}

const refusalSubject = `(?:i|we)(?:\s+(?:can't|cannot|can not|won't|am unable to|am not able to)|'m\s+(?:unable to|not able to)|'re\s+(?:unable to|not able to))`

var refusalPattern = regexp.MustCompile(`^(?:` +
	`(?:(?:i'm sorry|i am sorry|sorry|i apologi[sz]e)[,.!]?\s+(?:but\s+)?)?` + refusalSubject +
	`(?:\s+(?:help|assist|comply|provide|fulfill|fulfil|answer|do that|support)|[.!]?$)` +
	`|i must decline` +
	`)`)

var hedgePattern = regexp.MustCompile(`\b(?:but|however|though|here)\b`)

// SilentFailure classifies a span that did not error but still failed: cut off
// by the token limit, refused or filtered, or a tool that returned an error
// payload. Returns "" for a healthy span.
func SilentFailure(attrs map[string]string, kind, finishReason string, errored bool) string {
	if errored {
		return ""
	}
	reason := normalizeReason(finishReason)
	switch {
	case filterReasons[reason], reason == "incomplete" && incompleteFiltered(attrs):
		return SilentRefused
	case truncationReasons[reason]:
		return SilentTruncated
	}
	switch kind {
	case "llm":
		if isRefusal(llmOutput(attrs)) {
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

// incompleteFiltered tells a Responses API "incomplete" cut short by the
// content filter apart from one that ran out of tokens.
func incompleteFiltered(attrs map[string]string) bool {
	if strings.Contains(attrs[attrFinishReasons], "content_filter") {
		return true
	}
	var resp struct {
		IncompleteDetails struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
	}
	return json.Unmarshal([]byte(attrs[attrOpenInferenceOut]), &resp) == nil &&
		resp.IncompleteDetails.Reason == "content_filter"
}

func llmOutput(attrs map[string]string) string {
	if out := outputText(attrs); out != "" {
		return out
	}
	if out := attrs[attrOpenInferenceMsg]; out != "" {
		return out
	}
	return attrs[attrOpenInferenceOut]
}

// isRefusal only judges short outputs and skips a refusal phrase whose sentence
// goes on to hedge or answer ("I can't help but notice", "..., but here is").
func isRefusal(out string) bool {
	out = strings.TrimSpace(out)
	out = strings.TrimSpace(strings.TrimPrefix(out, "assistant:"))
	if len(out) > maxRefusalChars {
		return false
	}
	out = strings.ToLower(strings.ReplaceAll(out, "’", "'"))
	loc := refusalPattern.FindStringIndex(out[:min(len(out), refusalWindow)])
	if loc == nil {
		return false
	}
	rest := out[loc[1]:]
	if i := strings.IndexAny(rest, ".!?\n"); i >= 0 {
		rest = rest[:i]
	}
	return !hedgePattern.MatchString(rest)
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
	if len(out) > maxToolPayload {
		return false
	}
	if strings.HasPrefix(out, `"`) {
		var inner string
		if json.Unmarshal([]byte(out), &inner) != nil {
			return false
		}
		out = strings.TrimSpace(inner)
	}
	if !strings.HasPrefix(out, "{") {
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
	for _, k := range []string{"isError", "is_error"} {
		if obj[k] == true {
			return true
		}
	}
	for _, k := range []string{"success", "ok"} {
		if obj[k] == false {
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
			switch strings.ToLower(v) {
			case "error", "failed", "failure":
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
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "", "none", "null", "false", "ok", "0":
			return false
		}
		return true
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	}
	return true
}
