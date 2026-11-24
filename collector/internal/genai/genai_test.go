package genai

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestContent_OTelMessages(t *testing.T) {
	attrs := map[string]string{
		attrSystemInstructions: "be terse",
		attrInputMessages:      `[{"role":"user","parts":[{"type":"text","content":"hi"}]}]`,
		attrOutputMessages:     `[{"role":"assistant","parts":[{"type":"text","content":"hello"}]}]`,
	}
	in, out := Content(attrs)
	if want := "system: be terse\nuser: hi"; in != want {
		t.Errorf("input = %q, want %q", in, want)
	}
	if want := "assistant: hello"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestContent_OTelMessages_FlatContentFallback(t *testing.T) {
	attrs := map[string]string{
		attrInputMessages:  `[{"role":"user","content":"flat in"}]`,
		attrOutputMessages: `[{"role":"assistant","content":"flat out"}]`,
	}
	in, out := Content(attrs)
	if in != "user: flat in" {
		t.Errorf("input = %q", in)
	}
	if out != "assistant: flat out" {
		t.Errorf("output = %q", out)
	}
}

func TestContent_OpenLLMetryIndexed(t *testing.T) {
	attrs := map[string]string{
		"gen_ai.prompt.0.role":        "system",
		"gen_ai.prompt.0.content":     "you are helpful",
		"gen_ai.prompt.1.role":        "user",
		"gen_ai.prompt.1.content":     "2+2?",
		"gen_ai.completion.0.role":    "assistant",
		"gen_ai.completion.0.content": "4",
	}
	in, out := Content(attrs)
	if want := "system: you are helpful\nuser: 2+2?"; in != want {
		t.Errorf("input = %q, want %q", in, want)
	}
	if out != "assistant: 4" {
		t.Errorf("output = %q", out)
	}
}

func TestContent_ToolCallArgumentsAsOutput(t *testing.T) {
	for name, attrs := range map[string]map[string]string{
		"semconv parts":     {"gen_ai.output.messages": `[{"role":"assistant","parts":[{"type":"tool_call","name":"extract_fields","arguments":{"total":12}}]}]`},
		"openai tool_calls": {"gen_ai.output.messages": `[{"role":"assistant","content":"","tool_calls":[{"function":{"name":"extract_fields","arguments":"{\"total\":12}"}}]}]`},
		"indexed": {
			"gen_ai.completion.0.role":                   "assistant",
			"gen_ai.completion.0.tool_calls.0.name":      "extract_fields",
			"gen_ai.completion.0.tool_calls.0.arguments": `{"total":12}`,
		},
	} {
		if _, out := Content(attrs); out != `assistant: {"total":12}` {
			t.Errorf("%s: output = %q", name, out)
		}
	}
	text := map[string]string{"gen_ai.output.messages": `[{"role":"assistant","content":"Saving.","tool_calls":[{"name":"save","arguments":"{}"}]}]`}
	if _, out := Content(text); out != "assistant: Saving." {
		t.Errorf("text wins over arguments: %q", out)
	}
}

func TestContent_PrefersOTelOverIndexed(t *testing.T) {
	// When both shapes are present, the current OTel shape wins for output/input.
	attrs := map[string]string{
		attrInputMessages:      `[{"role":"user","content":"new"}]`,
		"gen_ai.prompt.0.role": "user", "gen_ai.prompt.0.content": "legacy",
	}
	in, _ := Content(attrs)
	if in != "user: new" {
		t.Errorf("input = %q, want OTel shape to win", in)
	}
}

func TestContent_Empty(t *testing.T) {
	in, out := Content(map[string]string{})
	if in != "" || out != "" {
		t.Errorf("expected empty, got in=%q out=%q", in, out)
	}
}

func TestContent_NonJSONMessagesPassThrough(t *testing.T) {
	attrs := map[string]string{attrInputMessages: "just a string"}
	in, _ := Content(attrs)
	if in != "just a string" {
		t.Errorf("input = %q, want raw passthrough", in)
	}
}

func TestContent_TraceloopEntity(t *testing.T) {
	// Structural decorator spans carry their wrapped function's args/result.
	attrs := map[string]string{
		attrEntityInput:  `{"args": ["A1001"], "kwargs": {}}`,
		attrEntityOutput: `{"status": "delivered"}`,
	}
	in, out := Content(attrs)
	if in != `{"args": ["A1001"], "kwargs": {}}` {
		t.Errorf("input = %q, want entity input", in)
	}
	if out != `{"status": "delivered"}` {
		t.Errorf("output = %q, want entity output", out)
	}
}

func TestContent_PrefersGenAIOverEntity(t *testing.T) {
	// An LLM span carries both: gen_ai.* content wins over the entity fallback.
	attrs := map[string]string{
		"gen_ai.prompt.0.role": "user", "gen_ai.prompt.0.content": "hello",
		attrEntityInput: `{"args": ["ignored"]}`,
	}
	if in, _ := Content(attrs); in != "user: hello" {
		t.Errorf("input = %q, want gen_ai content to win", in)
	}
}

func TestToolsJSON_OpenLLMetryDefinitions(t *testing.T) {
	attrs := map[string]string{
		"llm.request.functions.0.name":        "get_weather",
		"llm.request.functions.0.description": "look up weather",
		"llm.request.functions.1.name":        "get_time",
		"llm.request.functions.1.description": "current time",
		// model called only get_weather (OpenLLMetry tool_calls shape).
		"gen_ai.completion.0.role":              "assistant",
		"gen_ai.completion.0.tool_calls.0.name": "get_weather",
	}
	got := decodeTools(t, ToolsJSON(attrs))
	want := []Tool{
		{Name: "get_weather", Description: "look up weather", Used: true},
		{Name: "get_time", Description: "current time", Used: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tools = %+v, want %+v", got, want)
	}
}

func TestToolsJSON_SemconvDefinitions(t *testing.T) {
	attrs := map[string]string{
		attrToolDefinitions: `[
			{"type":"function","name":"get_weather","description":"look up weather"},
			{"type":"function","function":{"name":"get_time","description":"current time"}}
		]`,
		attrOutputMessages: `[{"role":"assistant","tool_calls":[{"function":{"name":"get_weather"}}]}]`,
	}
	got := decodeTools(t, ToolsJSON(attrs))
	want := []Tool{
		{Name: "get_weather", Description: "look up weather", Used: true},
		{Name: "get_time", Description: "current time", Used: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tools = %+v, want %+v", got, want)
	}
}

// The semconv array wins, but the legacy attributes must still work on their own.
func TestToolsJSON_SemconvPreferredOverIndexed(t *testing.T) {
	attrs := map[string]string{
		attrToolDefinitions:            `[{"name":"modern"}]`,
		"llm.request.functions.0.name": "legacy",
	}
	got := decodeTools(t, ToolsJSON(attrs))
	if len(got) != 1 || got[0].Name != "modern" {
		t.Errorf("tools = %+v, want the semconv definitions", got)
	}
}

// A client-controlled string must never produce garbage or a panic; bad input
// falls through to the legacy reader.
func TestToolsJSON_MalformedSemconvDefinitions(t *testing.T) {
	bad := []string{
		`not json`,
		`{"name":"obj-not-array"}`,
		`["just a string"]`,
		`[{"name":""},{"description":"unnamed"}]`,
		`[` + strings.Repeat("x", maxToolDefsBytes) + `]`,
	}
	for _, defs := range bad {
		if got := ToolsJSON(map[string]string{attrToolDefinitions: defs}); got != "" {
			t.Errorf("ToolsJSON(%.30q) = %q, want empty", defs, got)
		}
		attrs := map[string]string{
			attrToolDefinitions:            defs,
			"llm.request.functions.0.name": "legacy",
		}
		got := decodeTools(t, ToolsJSON(attrs))
		if len(got) != 1 || got[0].Name != "legacy" {
			t.Errorf("ToolsJSON(%.30q) = %+v, want the legacy fallback", defs, got)
		}
	}
}

func TestToolsJSON_UsedFromFunctionNameShape(t *testing.T) {
	attrs := map[string]string{
		"llm.request.functions.0.name":                   "search",
		"gen_ai.completion.0.finish_reason":              "tool_calls",
		"gen_ai.completion.0.tool_calls.0.function.name": "search",
	}
	got := decodeTools(t, ToolsJSON(attrs))
	if len(got) != 1 || !got[0].Used {
		t.Errorf("expected search marked used, got %+v", got)
	}
}

func TestToolsJSON_UsedFromOTelOutputMessages(t *testing.T) {
	attrs := map[string]string{
		"llm.request.functions.0.name": "lookup",
		attrOutputMessages:             `[{"role":"assistant","tool_calls":[{"function":{"name":"lookup"}}]}]`,
	}
	got := decodeTools(t, ToolsJSON(attrs))
	if len(got) != 1 || !got[0].Used {
		t.Errorf("expected lookup marked used, got %+v", got)
	}
}

func TestToolsJSON_NoTools(t *testing.T) {
	if got := ToolsJSON(map[string]string{}); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestIsContentKey(t *testing.T) {
	content := []string{
		attrInputMessages, attrOutputMessages, attrSystemInstructions,
		attrEntityInput, attrEntityOutput,
		"gen_ai.prompt.0.content", "gen_ai.completion.3.role",
	}
	for _, k := range content {
		if !IsContentKey(k) {
			t.Errorf("IsContentKey(%q) = false, want true", k)
		}
	}
	// Tool definitions are metadata, not content — must be kept.
	notContent := []string{
		"llm.request.functions.0.name", "gen_ai.request.model",
		"tracium.available_tools", "gen_ai.usage.input_tokens",
	}
	for _, k := range notContent {
		if IsContentKey(k) {
			t.Errorf("IsContentKey(%q) = true, want false", k)
		}
	}
}

func decodeTools(t *testing.T, s string) []Tool {
	t.Helper()
	if s == "" {
		return nil
	}
	var tools []Tool
	if err := json.Unmarshal([]byte(s), &tools); err != nil {
		t.Fatalf("invalid tools JSON %q: %v", s, err)
	}
	return tools
}
