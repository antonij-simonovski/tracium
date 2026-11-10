package genai

import (
	"strings"
	"testing"
)

func TestSilentFailure(t *testing.T) {
	out := func(text string) map[string]string {
		return map[string]string{"gen_ai.completion.0.role": "assistant", "gen_ai.completion.0.content": text}
	}
	tool := func(text string) map[string]string {
		return map[string]string{"traceloop.entity.output": text}
	}
	tests := []struct {
		name    string
		attrs   map[string]string
		kind    string
		finish  string
		errored bool
		want    string
	}{
		{"openai length", nil, "llm", "length", false, SilentTruncated},
		{"anthropic max_tokens", nil, "llm", "max_tokens", false, SilentTruncated},
		{"gemini MAX_TOKENS", nil, "llm", "MAX_TOKENS", false, SilentTruncated},
		{"gemini enum form", nil, "llm", "FINISH_REASON_MAX_TOKENS", false, SilentTruncated},
		{"responses api", nil, "llm", "max_output_tokens", false, SilentTruncated},
		{"stop is fine", out("Paris."), "llm", "stop", false, ""},
		{"errored span not flagged", nil, "llm", "length", true, ""},
		{"openai content_filter", nil, "llm", "content_filter", false, SilentRefused},
		{"gemini SAFETY", nil, "llm", "SAFETY", false, SilentRefused},
		{"anthropic refusal", nil, "llm", "refusal", false, SilentRefused},
		{"bedrock guardrail", nil, "llm", "guardrail_intervened", false, SilentRefused},
		{"refusal phrase", out("I'm sorry, but I can't help with that request."), "llm", "stop", false, SilentRefused},
		{"curly apostrophe", out("I’m sorry, I can’t assist with that."), "llm", "end_turn", false, SilentRefused},
		{"cannot assist", out("I cannot assist with creating malware."), "llm", "stop", false, SilentRefused},
		{"messages json", map[string]string{"gen_ai.output.messages": `[{"role":"assistant","content":"I can't help with that."}]`}, "llm", "", false, SilentRefused},
		{"short valid answer", out("Yes."), "llm", "stop", false, ""},
		{"sorry for delay", out("Sorry for the wait! Here is your summary."), "llm", "stop", false, ""},
		{"can't believe", out("I can't believe how simple this is: x = 2."), "llm", "stop", false, ""},
		{"refusal late in text", out("Here are the steps. Step 3: if asked to share keys, say I'm sorry, but I can't."), "llm", "stop", false, ""},
		{"refusal phrase on tool ignored", tool("I can't help with that"), "tool", "", false, ""},
		{"tool error key", tool(`{"error": "rate limited"}`), "tool", "", false, SilentToolError},
		{"tool error object", tool(`{"error": {"code": 500, "message": "boom"}}`), "tool", "", false, SilentToolError},
		{"tool exception", tool(`{"exception": "KeyError: 'id'"}`), "tool", "", false, SilentToolError},
		{"tool status 503", map[string]string{"gen_ai.tool.call.result": `{"status": 503, "body": "unavailable"}`}, "tool", "", false, SilentToolError},
		{"tool status_code 404", map[string]string{"output.value": `{"status_code": 404}`}, "tool", "", false, SilentToolError},
		{"tool status error", tool(`{"status": "error", "message": "bad input"}`), "tool", "", false, SilentToolError},
		{"tool errors array", tool(`{"data": null, "errors": [{"message": "not found"}]}`), "tool", "", false, SilentToolError},
		{"tool mentions error", tool(`{"result": "No error found in the logs"}`), "tool", "", false, ""},
		{"tool prose error", tool("error handling is described in section 4"), "tool", "", false, ""},
		{"tool null error", tool(`{"error": null, "data": [1]}`), "tool", "", false, ""},
		{"tool empty errors", tool(`{"errors": [], "data": [1]}`), "tool", "", false, ""},
		{"tool zero error", tool(`{"error": 0, "data": [1]}`), "tool", "", false, ""},
		{"tool false error", tool(`{"error": false}`), "tool", "", false, ""},
		{"tool status 200", tool(`{"status": 200, "items": []}`), "tool", "", false, ""},
		{"tool status ok", tool(`{"status": "ok"}`), "tool", "", false, ""},
		{"tool errored span", tool(`{"error": "x"}`), "tool", "", true, ""},
		{"error payload on llm ignored", out(`{"error": "x"}`), "llm", "stop", false, ""},
		{"i'm unable", out("I'm unable to help with that request."), "llm", "stop", false, SilentRefused},
		{"sorry i'm unable", out("I'm sorry, but I'm unable to assist with that."), "llm", "stop", false, SilentRefused},
		{"i'm not able", out("I'm not able to provide that information."), "llm", "stop", false, SilentRefused},
		{"we're unable", out("We're unable to assist with that."), "llm", "stop", false, SilentRefused},
		{"sorry but can't", out("I'm sorry, but I can't help with that."), "llm", "stop", false, SilentRefused},
		{"openinference message", map[string]string{"llm.output_messages.0.message.content": "I'm unable to help with that."}, "llm", "stop", false, SilentRefused},
		{"openinference output value", map[string]string{"output.value": "I cannot assist with that."}, "llm", "stop", false, SilentRefused},
		{"can't help but notice", out("I can't help but notice the query has a typo; here's the fixed SQL: SELECT 1;"), "llm", "stop", false, ""},
		{"hedged diagnosis", out("I cannot provide a definitive diagnosis, but common causes are A, B and C."), "llm", "stop", false, ""},
		{"however hedge", out("I can't provide exact figures; however the trend is up."), "llm", "stop", false, ""},
		{"long answer with caveat", out("I can't provide legal advice. " + strings.Repeat("General background follows. ", 30)), "llm", "stop", false, ""},
		{"responses incomplete", nil, "llm", "incomplete", false, SilentTruncated},
		{"responses incomplete filtered", map[string]string{"output.value": `{"status":"incomplete","incomplete_details":{"reason":"content_filter"}}`}, "llm", "incomplete", false, SilentRefused},
		{"incomplete filtered finish reasons", map[string]string{"gen_ai.response.finish_reasons": `["incomplete","content_filter"]`}, "llm", "incomplete", false, SilentRefused},
		{"filter reason on long output", out(strings.Repeat("x", 700)), "llm", "content_filter", false, SilentRefused},
		{"mcp isError", tool(`{"content":[{"type":"text","text":"ENOENT"}],"isError":true}`), "tool", "", false, SilentToolError},
		{"mcp isError false", tool(`{"content":[{"type":"text","text":"ok"}],"isError":false}`), "tool", "", false, ""},
		{"is_error", tool(`{"is_error": true}`), "tool", "", false, SilentToolError},
		{"success false", tool(`{"success": false, "message": "timeout"}`), "tool", "", false, SilentToolError},
		{"ok false", tool(`{"ok": false}`), "tool", "", false, SilentToolError},
		{"success string false not bool", tool(`{"success": "false"}`), "tool", "", false, ""},
		{"success true", tool(`{"success": true}`), "tool", "", false, ""},
		{"status failed", tool(`{"status": "failed", "reason": "quota"}`), "tool", "", false, SilentToolError},
		{"status Failure", tool(`{"status": "Failure"}`), "tool", "", false, SilentToolError},
		{"double encoded", tool(`"{\"error\": \"rate limited\"}"`), "tool", "", false, SilentToolError},
		{"double encoded ok", tool(`"{\"data\": 1}"`), "tool", "", false, ""},
		{"quoted prose", tool(`"error"`), "tool", "", false, ""},
		{"error none", tool(`{"error": "none"}`), "tool", "", false, ""},
		{"error NULL", tool(`{"error": " NULL "}`), "tool", "", false, ""},
		{"error ok", tool(`{"error": "ok"}`), "tool", "", false, ""},
		{"error string 0", tool(`{"error": "0"}`), "tool", "", false, ""},
		{"error string false", tool(`{"error": "False"}`), "tool", "", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SilentFailure(tt.attrs, tt.kind, tt.finish, tt.errored); got != tt.want {
				t.Errorf("SilentFailure = %q, want %q", got, tt.want)
			}
		})
	}
}
