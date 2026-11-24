package traciumprocessor

import (
	"math"
	"testing"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

func TestToSpanModelReadsReportedCostOnlyWithoutModel(t *testing.T) {
	s := ptrace.NewSpan()
	s.Attributes().PutDouble(attrCostUSD, 0.25)
	if got := toSpanModel(s, pcommon.NewMap(), attrMap(s.Attributes())).ReportedCostUSD; got != 0.25 {
		t.Errorf("tool span: reported cost = %v, want 0.25", got)
	}

	s.Attributes().PutStr(attrCostUSD, "$0.25")
	if got := toSpanModel(s, pcommon.NewMap(), attrMap(s.Attributes())).ReportedCostUSD; !math.IsNaN(got) {
		t.Errorf("unparseable cost = %v, want NaN so validation rejects it", got)
	}

	s.Attributes().PutDouble(attrCostUSD, 0.25)
	s.Attributes().PutInt("gen_ai.usage.input_tokens", 100)
	if got := toSpanModel(s, pcommon.NewMap(), attrMap(s.Attributes())).ReportedCostUSD; got != 0 {
		t.Errorf("span with tokens but no model: reported cost = %v, want 0", got)
	}
	s.Attributes().Remove("gen_ai.usage.input_tokens")

	s.Attributes().PutStr(attrModelRequest, "gpt-4o")
	if got := toSpanModel(s, pcommon.NewMap(), attrMap(s.Attributes())).ReportedCostUSD; got != 0 {
		t.Errorf("model span: reported cost = %v, want 0", got)
	}
}

// stripContent must remove raw prompt/completion content in both the current
// OTel and legacy OpenLLMetry shapes (used when content capture is disabled),
// while leaving non-content attributes — including tool definitions — untouched.
func TestStripContentRemovesOnlyRawContent(t *testing.T) {
	span := ptrace.NewSpan()
	attrs := span.Attributes()
	// OTel shape.
	attrs.PutStr("gen_ai.input.messages", `[{"role":"user","content":"hi"}]`)
	attrs.PutStr("gen_ai.output.messages", `[{"role":"assistant","content":"yo"}]`)
	attrs.PutStr("gen_ai.system_instructions", "be terse")
	// OpenLLMetry indexed shape.
	attrs.PutStr("gen_ai.prompt.0.content", "secret prompt")
	attrs.PutStr("gen_ai.completion.0.content", "secret completion")
	// Metadata that must survive.
	attrs.PutStr("llm.request.functions.0.name", "search")
	attrs.PutStr(attrAvailableTools, `[{"name":"search","used":false}]`)
	attrs.PutStr(attrModelRequest, "gpt-4o")

	stripContent(span)

	for _, k := range []string{
		"gen_ai.input.messages", "gen_ai.output.messages", "gen_ai.system_instructions",
		"gen_ai.prompt.0.content", "gen_ai.completion.0.content",
	} {
		if _, ok := attrs.Get(k); ok {
			t.Errorf("%s should be stripped", k)
		}
	}
	for _, k := range []string{
		"llm.request.functions.0.name", attrAvailableTools, attrModelRequest,
	} {
		if _, ok := attrs.Get(k); !ok {
			t.Errorf("%s must be preserved (not raw content)", k)
		}
	}
}

// stampAvailableTools computes tracium.available_tools from the upstream tool
// definitions and tool-call signals, regardless of content capture.
func TestStampAvailableTools(t *testing.T) {
	span := ptrace.NewSpan()
	attrs := span.Attributes()
	attrs.PutStr("llm.request.functions.0.name", "get_weather")
	attrs.PutStr("llm.request.functions.0.description", "look up weather")
	attrs.PutStr("gen_ai.completion.0.tool_calls.0.name", "get_weather")

	stampAvailableTools(span, attrMap(attrs))

	v, ok := attrs.Get(attrAvailableTools)
	if !ok {
		t.Fatal("tracium.available_tools should be stamped")
	}
	if want := `[{"name":"get_weather","description":"look up weather","used":true}]`; v.AsString() != want {
		t.Errorf("available_tools = %q, want %q", v.AsString(), want)
	}
}

// No tool definitions means no tracium.available_tools attribute.
func TestStampAvailableToolsNoToolsIsNoOp(t *testing.T) {
	span := ptrace.NewSpan()
	span.Attributes().PutStr(attrModelRequest, "gpt-4o")

	stampAvailableTools(span, attrMap(span.Attributes()))

	if _, ok := span.Attributes().Get(attrAvailableTools); ok {
		t.Error("available_tools must be absent when no tools were offered")
	}
}

// The verdict is read from content before stripping, and a sender cannot forge it.
func TestStampSilentFailure(t *testing.T) {
	span := ptrace.NewSpan()
	attrs := span.Attributes()
	attrs.PutStr(attrModelRequest, "gpt-4o")
	attrs.PutStr("gen_ai.completion.0.content", "I'm sorry, but I can't help with that.")
	stampSilentFailure(span, attrMap(attrs), toSpanModel(span, pcommon.NewMap(), attrMap(attrs)))
	if v, _ := attrs.Get(attrSilentFailure); v.AsString() != "refused" {
		t.Errorf("silent_failure = %q, want refused", v.AsString())
	}

	healthy := ptrace.NewSpan()
	healthy.Attributes().PutStr(attrModelRequest, "gpt-4o")
	healthy.Attributes().PutStr(attrSilentFailure, "truncated")
	stampSilentFailure(healthy, attrMap(healthy.Attributes()), toSpanModel(healthy, pcommon.NewMap(), attrMap(healthy.Attributes())))
	if _, ok := healthy.Attributes().Get(attrSilentFailure); ok {
		t.Error("sender-supplied silent_failure must be removed")
	}
}

func TestStampContentHashes(t *testing.T) {
	span := ptrace.NewSpan()
	attrs := span.Attributes()
	attrs.PutStr(attrModelRequest, "gpt-4o")
	attrs.PutStr("gen_ai.prompt.0.content", "Summarise the attached quarterly report for the board.")
	attrs.PutStr("gen_ai.completion.0.content", "Revenue grew eight percent while costs stayed flat this quarter.")
	stampContentHashes(span, attrMap(attrs), toSpanModel(span, pcommon.NewMap(), attrMap(attrs)))
	in, okIn := attrs.Get(attrInputHash)
	out, okOut := attrs.Get(attrOutputHash)
	if !okIn || !okOut || in.Int() == 0 || out.Int() == 0 || in.Int() == out.Int() {
		t.Errorf("hashes = %v/%v, want two distinct non-zero values", in.AsString(), out.AsString())
	}

	tool := ptrace.NewSpan()
	tool.Attributes().PutStr("openinference.span.kind", "TOOL")
	tool.Attributes().PutStr("output.value", "Revenue grew eight percent while costs stayed flat this quarter.")
	tool.Attributes().PutInt(attrOutputHash, 42)
	stampContentHashes(tool, attrMap(tool.Attributes()), toSpanModel(tool, pcommon.NewMap(), attrMap(tool.Attributes())))
	if _, ok := tool.Attributes().Get(attrOutputHash); ok {
		t.Error("non-LLM span and sender-supplied hash must leave no output_hash")
	}
}
