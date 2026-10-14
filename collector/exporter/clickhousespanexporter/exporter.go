package clickhousespanexporter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/tracium/collector/internal/genai"
	"github.com/tracium/collector/internal/ingest"
	"github.com/tracium/collector/internal/writer"
	"github.com/tracium/collector/pkg/spanmodel"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type chExporter struct {
	dsn    string
	writer *writer.ClickHouseWriter
}

func (e *chExporter) start(ctx context.Context, _ component.Host) error {
	w, err := writer.NewClickHouseWriter(ctx, e.dsn)
	if err != nil {
		return fmt.Errorf("clickhousespan: %w", err)
	}
	e.writer = w
	return nil
}

func (e *chExporter) shutdown(context.Context) error {
	if e.writer == nil {
		return nil
	}
	return e.writer.Close()
}

// pushTraces flattens an OTLP batch into spanmodel.Span rows and writes them in
// a single ClickHouse batch. It expects spans already enriched by the tracium
// processor (tracium.* attributes present).
func (e *chExporter) pushTraces(ctx context.Context, td ptrace.Traces) error {
	spans := make([]*spanmodel.Span, 0, td.SpanCount())

	rss := td.ResourceSpans()
	for i := 0; i < rss.Len(); i++ {
		resourceAttrs := rss.At(i).Resource().Attributes()
		serviceName := strAttr(resourceAttrs, attrServiceName)
		sss := rss.At(i).ScopeSpans()
		for j := 0; j < sss.Len(); j++ {
			ss := sss.At(j).Spans()
			for k := 0; k < ss.Len(); k++ {
				spans = append(spans, fromOTLP(ss.At(k), serviceName, resourceAttrs))
			}
		}
	}
	err := e.writer.WriteBatch(ctx, spans)
	if errors.Is(err, writer.ErrRowRejected) {
		return consumererror.NewPermanent(err)
	}
	return err
}

// Attribute keys mirror traciumprocessor, so the exporter also works for spans
// that did not pass through it.
const (
	attrModelRequest    = "gen_ai.request.model"
	attrModelResponse   = "gen_ai.response.model"
	attrFinishReason    = "gen_ai.response.finish_reasons"
	attrUserID          = "tracium.user.id"
	attrWorkspaceID     = "tracium.workspace.id"
	attrCostUSD         = "tracium.cost_usd"
	attrModelNormalized = "tracium.model_normalized"
	attrSchemaVersion   = "tracium.schema_version"
	attrAvailableTools  = "tracium.available_tools"
	attrUnmetered       = "tracium.usage.unmetered"

	attrServiceName       = "service.name"
	attrGenAIAgentName    = "gen_ai.agent.name"
	attrTraceloopWorkflow = "traceloop.workflow.name"
	attrTraceloopEntity   = "traceloop.entity.name"
	attrWorkflowVersion   = "tracium.workflow.version"
)

func fromOTLP(s ptrace.Span, serviceName string, resourceAttrs pcommon.Map) *spanmodel.Span {
	attrs := s.Attributes()
	model := strAttr(attrs, attrModelRequest)
	if model == "" {
		model = strAttr(attrs, attrModelResponse)
	}
	am := attrMap(attrs)
	usage := genai.Usage(am)
	startMs := s.StartTimestamp().AsTime().UnixMilli()
	endMs := s.EndTimestamp().AsTime().UnixMilli()

	// Empty when the processor stripped content because capture is disabled.
	input, output := genai.Content(am)
	errType, errMessage := spanError(s)

	return &spanmodel.Span{
		TraceID:         s.TraceID().String(),
		SpanID:          s.SpanID().String(),
		ParentSpanID:    s.ParentSpanID().String(),
		Name:            s.Name(),
		StartTimeMs:     startMs,
		EndTimeMs:       endMs,
		DurationMs:      endMs - startMs,
		Model:           model,
		ModelNormalized: strAttr(attrs, attrModelNormalized),
		InputTokens:     usage.InputTokens,
		OutputTokens:    usage.OutputTokens,
		// Both flags are metering provenance, not usage: they say how much of the
		// count above is trustworthy. Stored so a $0 span is distinguishable from
		// a genuinely free one.
		OutputTokensDerived: usage.OutputTokensDerived,
		Unmetered:           usage.Unmetered || boolAttr(attrs, attrUnmetered),
		FinishReason:        finishReason(attrs),
		Kind:                spanKind(attrs, model, usage.InputTokens, usage.OutputTokens),
		CostUSD:             floatAttr(attrs, attrCostUSD),
		UserID:              strAttr(attrs, attrUserID),
		WorkspaceID:         workspaceID(attrs, resourceAttrs),
		SchemaVersion:       int(intAttr(attrs, attrSchemaVersion)),
		ErrorType:           errType,
		ErrorMessage:        errMessage,
		WorkflowName: workflowName(
			serviceName,
			strAttr(attrs, attrGenAIAgentName),
			strAttr(attrs, attrTraceloopWorkflow),
			strAttr(attrs, attrTraceloopEntity),
			s.Name(),
		),
		WorkflowVersion: workflowVersion(attrs, resourceAttrs),
		// ServiceName is the resource-level service.name, persisted verbatim (the
		// OTel "unknown_service" default treated as absent). The query layer uses
		// it as the stable in-flight fallback for a trace's display/workflow name.
		ServiceName: resourceServiceName(serviceName),
		// Content is absent unless the processor kept it (capture enabled);
		// available_tools is collector-computed metadata, passed through verbatim.
		Input:          input,
		Output:         output,
		AvailableTools: strAttr(attrs, attrAvailableTools),
		Attributes:     customAttributes(resourceAttrs, attrs),
	}
}

// Ingest bounds for the client-controlled custom-attribute map, so no sender
// can bloat every row with thousands of oversized keys.
const (
	maxAttrs          = 64 // custom keys retained per span
	maxAttrKeyBytes   = 128
	maxAttrValueBytes = 256
)

// attrDenyPrefixes are the namespaces Tracium already promotes to typed columns
// or stores as content. Keeping them in the custom-attribute map would duplicate
// promoted data (model, tokens, cost, user, workflow) or store large
// prompt/completion text; everything outside them is a custom business attribute
// the operator can allocate by, retained verbatim.
var attrDenyPrefixes = []string{"gen_ai.", "tracium.", "llm.", "traceloop."}

// attrDenyKeys are promoted keys that don't share a denied prefix.
var attrDenyKeys = map[string]bool{attrServiceName: true}

// customAttributes merges a span's resource- and span-level OTLP attributes into
// the flat map Tracium stores for allocation, dropping the promoted/content
// namespaces and applying the ingest caps above. Span-level keys win over
// resource-level ones. Returns nil when nothing custom is present.
func customAttributes(resourceAttrs, spanAttrs pcommon.Map) map[string]string {
	out := make(map[string]string)
	add := func(m pcommon.Map) {
		m.Range(func(k string, v pcommon.Value) bool {
			if len(k) == 0 || len(k) > maxAttrKeyBytes || isDeniedAttr(k) {
				return true
			}
			// Cap the number of distinct keys, but always allow a span-level key
			// to overwrite a resource-level one already stored.
			if _, exists := out[k]; !exists && len(out) >= maxAttrs {
				return true
			}
			val := v.AsString()
			if len(val) > maxAttrValueBytes {
				val = strings.ToValidUTF8(val[:maxAttrValueBytes], "")
			}
			out[k] = val
			return true
		})
	}
	add(resourceAttrs)
	add(spanAttrs)
	if len(out) == 0 {
		return nil
	}
	return out
}

func isDeniedAttr(k string) bool {
	if attrDenyKeys[k] {
		return true
	}
	for _, p := range attrDenyPrefixes {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	return false
}

// workflowName picks the workflow that owns this span, preferring span-scoped
// signals over the resource-level service.name: gen_ai.agent.name,
// traceloop.workflow.name, traceloop.entity.name, then service.name (ignoring
// the SDK's "unknown_service" default), then the span name.
func workflowName(serviceName, genaiAgent, traceloopWorkflow, traceloopEntity, spanName string) string {
	for _, c := range []string{genaiAgent, traceloopWorkflow, traceloopEntity, resourceServiceName(serviceName), spanName} {
		if name := strings.TrimSpace(c); name != "" {
			return name
		}
	}
	return ""
}

// workflowVersion returns the span's tracium.workflow.version, else the resource's.
func workflowVersion(spanAttrs, resourceAttrs pcommon.Map) string {
	if v := ingest.SanitizeIdentifier(strAttr(spanAttrs, attrWorkflowVersion), ingest.MaxVersionBytes); v != "" {
		return v
	}
	return ingest.SanitizeIdentifier(strAttr(resourceAttrs, attrWorkflowVersion), ingest.MaxVersionBytes)
}

// resourceServiceName returns the usable service.name, treating the OTel SDK
// default "unknown_service" (optionally "unknown_service:<process>") as absent.
func resourceServiceName(serviceName string) string {
	if strings.HasPrefix(serviceName, "unknown_service") {
		return ""
	}
	return strings.TrimSpace(serviceName)
}

// OTel records failures as a first-class span status plus (optionally) an
// "exception" event carrying the type/message — never as a gen_ai.* attribute.
// These keys read that event; the constants follow the OTel exception semantic
// conventions.
const (
	eventException       = "exception"
	attrExceptionType    = "exception.type"
	attrExceptionMessage = "exception.message"
)

// spanError derives the error_type/error_message columns from an OTLP span.
// Returns empty strings unless the span's status code is Error, so successful
// and unset spans stay error-free. When present, an "exception" event supplies
// the most specific type/message (e.g. the SDK exception class and API error
// text); otherwise we fall back to a generic type and the status message.
func spanError(s ptrace.Span) (errType, errMessage string) {
	if s.Status().Code() != ptrace.StatusCodeError {
		return "", ""
	}

	events := s.Events()
	for i := 0; i < events.Len(); i++ {
		ev := events.At(i)
		if ev.Name() != eventException {
			continue
		}
		errType = strAttr(ev.Attributes(), attrExceptionType)
		errMessage = strAttr(ev.Attributes(), attrExceptionMessage)
		break
	}

	if errType == "" {
		errType = "error"
	}
	if errMessage == "" {
		errMessage = s.Status().Message()
	}
	return errType, errMessage
}

// attrMap snapshots a span's attributes as a plain string map for the
// framework-free genai helper.
func attrMap(m pcommon.Map) map[string]string {
	out := make(map[string]string, m.Len())
	m.Range(func(k string, v pcommon.Value) bool {
		out[k] = v.AsString()
		return true
	})
	return out
}

// finishReason reads gen_ai.response.finish_reasons, which the OTel GenAI
// semantic conventions define as an *array*. We store the first element: a span
// describes a single model call, so anything past the first is degenerate, and
// a scalar is what the finish_reason column's equality filters can match.
//
// Reading it with strAttr instead would JSON-encode the array to `["length"]`,
// which no dashboard filter or WHERE finish_reason = 'length' ever matches.
// Instrumentations that send a bare string are handled by the fallback.
// Duplicated in traciumprocessor, per this file's independent-usability rule.
func finishReason(attrs pcommon.Map) string {
	v, ok := attrs.Get(attrFinishReason)
	if !ok {
		return ""
	}
	if v.Type() != pcommon.ValueTypeSlice {
		return v.AsString()
	}
	if s := v.Slice(); s.Len() > 0 {
		return s.At(0).AsString()
	}
	return ""
}

// workspaceID reads tracium.workspace.id from the span, falling back to the
// resource for spans that did not pass through the processor.
func workspaceID(spanAttrs, resourceAttrs pcommon.Map) string {
	if v := strAttr(spanAttrs, attrWorkspaceID); v != "" {
		return v
	}
	return strAttr(resourceAttrs, attrWorkspaceID)
}

func strAttr(attrs pcommon.Map, key string) string {
	if v, ok := attrs.Get(key); ok {
		return v.AsString()
	}
	return ""
}

func intAttr(attrs pcommon.Map, key string) int64 {
	if v, ok := attrs.Get(key); ok {
		return v.Int()
	}
	return 0
}

func boolAttr(attrs pcommon.Map, key string) bool {
	v, ok := attrs.Get(key)
	return ok && v.Type() == pcommon.ValueTypeBool && v.Bool()
}

func floatAttr(attrs pcommon.Map, key string) float64 {
	if v, ok := attrs.Get(key); ok {
		return v.Double()
	}
	return 0
}
