package clickhousespanexporter

import (
	"fmt"
	"strings"
	"testing"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

// fromOTLP must surface OTel span failures as the error_type/error_message
// columns. OpenLLMetry (and any OTel SDK) reports a failed LLM call as a span
// with status code Error plus an "exception" event — never as a gen_ai.*
// attribute — so this is the only place the error text can come from.
func TestFromOTLP_MapsExceptionEventToError(t *testing.T) {
	s := ptrace.NewSpan()
	s.SetName("openai.chat")
	s.Status().SetCode(ptrace.StatusCodeError)
	s.Status().SetMessage("status fallback")

	ev := s.Events().AppendEmpty()
	ev.SetName("exception")
	ev.Attributes().PutStr("exception.type", "NotFoundError")
	ev.Attributes().PutStr("exception.message", "model_not_found: gpt-4o-miini")

	row := fromOTLP(s, "", pcommon.NewMap())

	if row.ErrorType != "NotFoundError" {
		t.Errorf("error_type = %q, want NotFoundError", row.ErrorType)
	}
	if row.ErrorMessage != "model_not_found: gpt-4o-miini" {
		t.Errorf("error_message = %q, want the exception message", row.ErrorMessage)
	}
}

// An errored span without an exception event still produces a non-empty
// error_type, so the trace registers as failed (has_error keys off error_type),
// falling back to the status message for the detail.
func TestFromOTLP_ErrorStatusWithoutExceptionEvent(t *testing.T) {
	s := ptrace.NewSpan()
	s.Status().SetCode(ptrace.StatusCodeError)
	s.Status().SetMessage("upstream timeout")

	row := fromOTLP(s, "", pcommon.NewMap())

	if row.ErrorType != "error" {
		t.Errorf("error_type = %q, want generic \"error\"", row.ErrorType)
	}
	if row.ErrorMessage != "upstream timeout" {
		t.Errorf("error_message = %q, want status message", row.ErrorMessage)
	}
}

// Successful and unset spans must stay error-free.
func TestFromOTLP_OkSpanHasNoError(t *testing.T) {
	s := ptrace.NewSpan()
	s.Status().SetCode(ptrace.StatusCodeOk)

	row := fromOTLP(s, "", pcommon.NewMap())

	if row.ErrorType != "" || row.ErrorMessage != "" {
		t.Errorf("ok span got error (%q, %q), want empty", row.ErrorType, row.ErrorMessage)
	}
}

// spanKind derives the normalized role from instrumentation attributes in
// precedence order, falling back to token/model presence and finally to "" when
// nothing classifies the span.
func TestSpanKind_DerivationPriority(t *testing.T) {
	tests := []struct {
		name                      string
		attrs                     map[string]string
		model                     string
		inputTokens, outputTokens int64
		want                      string
	}{
		{"openinference wins over everything", map[string]string{"openinference.span.kind": "RETRIEVER", "traceloop.span.kind": "tool", "gen_ai.operation.name": "chat"}, "gpt-4o", 10, 5, "retriever"},
		{"openinference reranker maps to retriever", map[string]string{"openinference.span.kind": "RERANKER"}, "", 0, 0, "retriever"},
		{"openinference is case-insensitive", map[string]string{"openinference.span.kind": "Chain"}, "", 0, 0, "chain"},
		{"openinference guardrail stays unclassified, falls through to tokens", map[string]string{"openinference.span.kind": "GUARDRAIL"}, "", 0, 0, ""},
		{"traceloop workflow maps to chain", map[string]string{"traceloop.span.kind": "workflow"}, "", 0, 0, "chain"},
		{"traceloop task maps to chain", map[string]string{"traceloop.span.kind": "task"}, "", 0, 0, "chain"},
		{"traceloop tool", map[string]string{"traceloop.span.kind": "tool"}, "", 0, 0, "tool"},
		{"gen_ai execute_tool maps to tool", map[string]string{"gen_ai.operation.name": "execute_tool"}, "", 0, 0, "tool"},
		{"gen_ai embeddings maps to embedding", map[string]string{"gen_ai.operation.name": "embeddings"}, "", 0, 0, "embedding"},
		{"gen_ai invoke_agent maps to agent", map[string]string{"gen_ai.operation.name": "invoke_agent"}, "", 0, 0, "agent"},
		{"model presence implies llm", nil, "gpt-4o", 0, 0, "llm"},
		{"token usage implies llm", nil, "", 0, 7, "llm"},
		{"nothing classifies yields empty", nil, "", 0, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := pcommon.NewMap()
			for k, v := range tt.attrs {
				attrs.PutStr(k, v)
			}
			if got := spanKind(attrs, tt.model, tt.inputTokens, tt.outputTokens); got != tt.want {
				t.Errorf("spanKind(%v, %q, %d, %d) = %q, want %q", tt.attrs, tt.model, tt.inputTokens, tt.outputTokens, got, tt.want)
			}
		})
	}
}

// fromOTLP must surface the classified kind on the row so it reaches ClickHouse.
func TestFromOTLP_SetsKind(t *testing.T) {
	s := ptrace.NewSpan()
	s.SetName("retrieve_docs")
	s.Attributes().PutStr("openinference.span.kind", "RETRIEVER")

	if row := fromOTLP(s, "", pcommon.NewMap()); row.Kind != "retriever" {
		t.Errorf("kind = %q, want retriever", row.Kind)
	}
}

// workflowName prefers span-scoped signals (gen_ai.agent.name, traceloop
// entity/workflow) over the resource-level service.name, so a trace made of many
// sub-spans attributes each span to its real workflow; the OTel "unknown_service"
// default is treated as absent so traces don't collapse under it (or under a raw
// operation name like "openai.chat").
func TestWorkflowName_DerivationPriority(t *testing.T) {
	tests := []struct {
		name                                                    string
		service, genaiAgent, traceloopWorkflow, traceloopEntity string
		spanName                                                string
		want                                                    string
	}{
		{"gen_ai.agent.name wins over service.name", "checkout-svc", "research-agent", "wf", "ent", "openai.chat", "research-agent"},
		{"span-scoped signal wins over the shared service", "weather-svc", "Weather Agent", "", "", "invoke_agent", "Weather Agent"},
		{"span with no span-scoped signal falls back to service.name", "weather-svc", "", "", "", "openai.chat", "weather-svc"},
		{"then traceloop.workflow.name over service.name", "svc", "", "summarize", "ent", "openai.chat", "summarize"},
		{"then traceloop.entity.name over service.name", "svc", "", "", "fetch_docs", "openai.chat", "fetch_docs"},
		{"service.name used when no span-scoped signal", "checkout-svc", "", "", "", "openai.chat", "checkout-svc"},
		{"span name as last resort", "", "", "", "", "openai.chat", "openai.chat"},
		{"unknown_service is treated as absent", "unknown_service", "", "", "", "openai.chat", "openai.chat"},
		{"unknown_service with process suffix is absent", "unknown_service:python", "", "", "", "openai.chat", "openai.chat"},
		{"whitespace-only signal is skipped", "   ", "agent", "", "", "openai.chat", "agent"},
		{"all empty yields empty", "", "", "", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := workflowName(tt.service, tt.genaiAgent, tt.traceloopWorkflow, tt.traceloopEntity, tt.spanName)
			if got != tt.want {
				t.Errorf("workflowName(%q,%q,%q,%q,%q) = %q, want %q",
					tt.service, tt.genaiAgent, tt.traceloopWorkflow, tt.traceloopEntity, tt.spanName, got, tt.want)
			}
		})
	}
}

func TestFromOTLP_WorkflowVersionPriority(t *testing.T) {
	tests := []struct {
		name            string
		span, resource  map[string]string
		version, source string
	}{
		{"span version wins over everything", map[string]string{"tracium.workflow.version": "v12", "traceloop.prompt.version": "3"}, map[string]string{"tracium.workflow.version": "v11", "service.version": "1.4.0"}, "v12", "explicit"},
		{"resource version when the span has none", nil, map[string]string{"tracium.workflow.version": "v11", "service.version": "1.4.0"}, "v11", "explicit"},
		{"prompt version with its key", map[string]string{"traceloop.prompt.key": "summarize", "traceloop.prompt.version": "3"}, map[string]string{"service.version": "1.4.0"}, "summarize@3", "prompt"},
		{"prompt version without a key", map[string]string{"traceloop.prompt.version": "3"}, nil, "3", "prompt"},
		{"prompt key alone is no version", map[string]string{"traceloop.prompt.key": "summarize"}, map[string]string{"service.version": "1.4.0"}, "1.4.0", "service"},
		{"service.version before the vcs revision", nil, map[string]string{"service.version": "1.4.0", "vcs.ref.head.revision": "9f2c1ab"}, "1.4.0", "service"},
		{"vcs revision as the last resort", nil, map[string]string{"vcs.ref.head.revision": "9f2c1ab"}, "9f2c1ab", "service"},
		{"blank values are skipped", map[string]string{"tracium.workflow.version": "  "}, map[string]string{"service.version": "1.4.0"}, "1.4.0", "service"},
		{"no version", nil, nil, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ptrace.NewSpan()
			for k, v := range tt.span {
				s.Attributes().PutStr(k, v)
			}
			resource := pcommon.NewMap()
			for k, v := range tt.resource {
				resource.PutStr(k, v)
			}
			row := fromOTLP(s, "", resource)
			if row.WorkflowVersion != tt.version || row.VersionSource != tt.source {
				t.Errorf("version = (%q, %q), want (%q, %q)", row.WorkflowVersion, row.VersionSource, tt.version, tt.source)
			}
		})
	}
}

func TestFromOTLP_SanitizesWorkflowVersion(t *testing.T) {
	s := ptrace.NewSpan()
	s.Attributes().PutStr("tracium.workflow.version", "v1\n"+strings.Repeat("x", 200))

	row := fromOTLP(s, "", pcommon.NewMap())

	if strings.Contains(row.WorkflowVersion, "\n") || len(row.WorkflowVersion) != 128 {
		t.Errorf("version = %q (%d bytes), want control characters dropped and 128 bytes", row.WorkflowVersion, len(row.WorkflowVersion))
	}
}

// fromOTLP persists the resource service.name on its own column, storing the
// OTel "unknown_service" default as empty so it never becomes a stable fallback.
func TestFromOTLP_ServiceNameColumn(t *testing.T) {
	tests := []struct {
		name    string
		service string
		want    string
	}{
		{"real service name kept", "weather-svc", "weather-svc"},
		{"unknown_service stored empty", "unknown_service", ""},
		{"unknown_service with suffix stored empty", "unknown_service:python", ""},
		{"empty stays empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := fromOTLP(ptrace.NewSpan(), tt.service, pcommon.NewMap())
			if row.ServiceName != tt.want {
				t.Errorf("ServiceName = %q, want %q", row.ServiceName, tt.want)
			}
		})
	}
}

// fromOTLP retains custom business attributes (the ones the operator allocates
// by) from both resource and span level, while excluding the promoted/content
// namespaces so they are not duplicated into the map.
func TestFromOTLP_RetainsCustomAttributes(t *testing.T) {
	res := pcommon.NewMap()
	res.PutStr("service.name", "billing-agent") // promoted → workflow, excluded
	res.PutStr("deployment.environment", "prod")
	res.PutStr("team", "platform")

	s := ptrace.NewSpan()
	s.Attributes().PutStr("gen_ai.request.model", "gpt-4o") // promoted, excluded
	s.Attributes().PutStr("tracium.cost_usd", "0.01")       // promoted, excluded
	s.Attributes().PutStr("user.id", "alice@example.com")
	s.Attributes().PutStr("team", "payments") // span overrides resource

	row := fromOTLP(s, "billing-agent", res)

	want := map[string]string{
		"deployment.environment": "prod",
		"team":                   "payments",
		"user.id":                "alice@example.com",
	}
	if len(row.Attributes) != len(want) {
		t.Fatalf("attributes = %v, want %v", row.Attributes, want)
	}
	for k, v := range want {
		if row.Attributes[k] != v {
			t.Errorf("attributes[%q] = %q, want %q", k, row.Attributes[k], v)
		}
	}
	for _, denied := range []string{"service.name", "gen_ai.request.model", "tracium.cost_usd"} {
		if _, ok := row.Attributes[denied]; ok {
			t.Errorf("promoted key %q leaked into attributes", denied)
		}
	}
}

// The attribute bag is capped so a client cannot bloat rows.
func TestFromOTLP_CapsAttributeCount(t *testing.T) {
	s := ptrace.NewSpan()
	for i := 0; i < maxAttrs+50; i++ {
		s.Attributes().PutStr(fmt.Sprintf("k%03d", i), "v")
	}
	row := fromOTLP(s, "", pcommon.NewMap())
	if len(row.Attributes) > maxAttrs {
		t.Errorf("retained %d attributes, cap is %d", len(row.Attributes), maxAttrs)
	}
}

func TestFromOTLP_ReadsUnmeteredStampedByProcessor(t *testing.T) {
	s := ptrace.NewSpan()
	s.Attributes().PutStr("gen_ai.request.model", "gpt-4o-mini")
	s.Attributes().PutBool(attrUnmetered, true)

	if row := fromOTLP(s, "", pcommon.NewMap()); !row.Unmetered {
		t.Error("unmetered = false, want true")
	}
}

func TestFromOTLP_IgnoresClientReportedCost(t *testing.T) {
	s := ptrace.NewSpan()
	s.Attributes().PutStr("gen_ai.usage.cost", "1000000")

	if row := fromOTLP(s, "", pcommon.NewMap()); row.CostUSD != 0 {
		t.Errorf("cost = %v, want 0", row.CostUSD)
	}
}
