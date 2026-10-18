package traciumprocessor

import (
	"context"

	"github.com/tracium/collector/enrich"
	"github.com/tracium/collector/internal/deadletter"
	customerrors "github.com/tracium/collector/internal/errors"
	"github.com/tracium/collector/internal/genai"
	"github.com/tracium/collector/pkg/spanmodel"

	"go.opentelemetry.io/collector/client"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// traciumProcessor maps OTLP trace spans to the Tracium span model, runs the
// enrichment chain, and writes the enriched values back as span attributes.
// Spans the chain drops are removed from the batch, counted and dead-lettered.
type traciumProcessor struct {
	logger         *zap.Logger
	chain          *enrich.Chain
	captureContent bool

	// deadLetter receives every dropped span with its error code attached.
	deadLetter deadletter.Store
	// dropped counts dropped spans by error code. The processor mutates the
	// batch in place, so processorhelper counts every span as accepted — this
	// counter is the only signal that anything was discarded.
	dropped metric.Int64Counter
}

// processTraces is the processorhelper ProcessTracesFunc. It never fails the
// batch: every span is either kept or dropped and dead-lettered.
func (p *traciumProcessor) processTraces(ctx context.Context, td ptrace.Traces) (ptrace.Traces, error) {
	// The traciumauth receiver authenticator normally rejects keyless requests
	// with 401; dropping them here is the backstop if it is not configured.
	scope := ingestScope(ctx)

	rss := td.ResourceSpans()
	for i := 0; i < rss.Len(); i++ {
		rs := rss.At(i)
		resourceAttrs := rs.Resource().Attributes()
		sss := rs.ScopeSpans()
		for j := 0; j < sss.Len(); j++ {
			sss.At(j).Spans().RemoveIf(func(otelSpan ptrace.Span) bool {
				attrs := attrMap(otelSpan.Attributes())
				model := toSpanModel(otelSpan, resourceAttrs, attrs)
				model.WorkspaceID = scope.workspace
				if !scope.authenticated {
					p.recordDrop(ctx, model, customerrors.InvalidSpan(
						customerrors.ErrUnauthenticated, "ingest requires a verified API key"))
					return true
				}
				if err := p.chain.Apply(ctx, model); err != nil {
					p.recordDrop(ctx, model, err)
					return true
				}
				writeBack(otelSpan, model)
				// Before stripping: tool-call signals live in the completion.
				stampAvailableTools(otelSpan, attrs)
				if !p.captureContent {
					stripContent(otelSpan)
				}
				return false
			})
		}
	}
	return td, nil
}

// authAttrWorkspace is the client.Info attribute key under which the traciumauth
// extension puts the verified key's workspace. It is a wire contract with that
// extension (kept as a literal string here to avoid a build-time dependency from
// the processor onto the extension module).
const authAttrWorkspace = "tracium.workspace"

// authScope is the workspace a verified ingest key resolved to. When
// authenticated is false the request carried no verified key, and the processor
// rejects every span in it — ingest is key-only, there is no keyless path.
type authScope struct {
	authenticated bool
	workspace     string
}

// ingestScope reads the authenticated ingest key's workspace off the request
// context. The traciumauth extension puts it on client.Info.Auth; any other case
// (no authenticator ran, a different authenticator, an empty value) yields a
// non-authenticated scope, which the caller rejects.
func ingestScope(ctx context.Context) authScope {
	auth := client.FromContext(ctx).Auth
	if auth == nil {
		return authScope{}
	}
	workspace, ok := auth.GetAttribute(authAttrWorkspace).(string)
	if !ok || workspace == "" {
		// Authenticated by something that is not our extension, or with no
		// workspace — not a valid ingest key.
		return authScope{}
	}
	return authScope{authenticated: true, workspace: workspace}
}

// recordDrop counts a dropped span by error code and hands it to the
// dead-letter store, so no span leaves the pipeline unrecorded.
func (p *traciumProcessor) recordDrop(ctx context.Context, span *spanmodel.Span, cause error) {
	code := customerrors.Code(cause)
	if code == "" {
		code = "unknown"
	}

	if p.dropped != nil {
		p.dropped.Add(ctx, 1, metric.WithAttributes(attribute.String("code", code)))
	}
	if err := p.deadLetter.Put(ctx, deadletter.Record{Code: code, Reason: cause.Error(), Span: span}); err != nil {
		// The span is already lost; an unusable sink is an operator problem, not
		// a reason to fail (and endlessly retry) the whole batch.
		p.logger.Error("dead-letter store rejected a span",
			zap.String("code", code), zap.Error(err))
	}
}

// Attribute keys: gen_ai.* follow OTel semantic conventions; tracium.* are the
// enriched outputs this processor produces.
const (
	attrModelRequest  = "gen_ai.request.model"
	attrModelResponse = "gen_ai.response.model"
	attrFinishReason  = "gen_ai.response.finish_reasons"

	attrUserID          = "tracium.user.id"
	attrWorkspaceID     = "tracium.workspace.id"
	attrCostUSD         = "tracium.cost_usd"
	attrModelNormalized = "tracium.model_normalized"
	attrSchemaVersion   = "tracium.schema_version"
	attrAvailableTools  = "tracium.available_tools"
	attrUnmetered       = "tracium.usage.unmetered"
)

// toSpanModel extracts the fields the enrichment chain needs from an OTLP span.
// attrs is the span's attribute map, snapshotted once by the caller.
func toSpanModel(s ptrace.Span, resourceAttrs pcommon.Map, attrs map[string]string) *spanmodel.Span {
	model := attrs[attrModelRequest]
	if model == "" {
		model = attrs[attrModelResponse]
	}

	// User may arrive on the span or on the resource.
	userID := attrs[attrUserID]
	if userID == "" {
		if v, ok := resourceAttrs.Get(attrUserID); ok {
			userID = v.AsString()
		}
	}

	startMs := s.StartTimestamp().AsTime().UnixMilli()
	endMs := s.EndTimestamp().AsTime().UnixMilli()
	usage := genai.Usage(attrs)

	return &spanmodel.Span{
		TraceID:          s.TraceID().String(),
		SpanID:           s.SpanID().String(),
		ParentSpanID:     s.ParentSpanID().String(),
		Name:             s.Name(),
		StartTimeMs:      startMs,
		EndTimeMs:        endMs,
		DurationMs:       endMs - startMs,
		Model:            model,
		InputTokens:      usage.InputTokens,
		OutputTokens:     usage.OutputTokens,
		CacheReadTokens:  usage.CacheReadTokens,
		CacheWriteTokens: usage.CacheWriteTokens,
		Unmetered:        usage.Unmetered,
		// The attrs map holds the JSON-encoded array, so read the raw value.
		FinishReason: finishReason(s.Attributes()),
		UserID:       userID,
	}
}

// writeBack stamps the enriched fields onto the OTLP span as attributes.
func writeBack(s ptrace.Span, m *spanmodel.Span) {
	attrs := s.Attributes()
	attrs.PutDouble(attrCostUSD, m.CostUSD)
	attrs.PutStr(attrModelNormalized, m.ModelNormalized)
	attrs.PutInt(attrSchemaVersion, int64(m.SchemaVersion))
	if m.UserID != "" {
		attrs.PutStr(attrUserID, m.UserID)
	}
	if m.WorkspaceID != "" {
		attrs.PutStr(attrWorkspaceID, m.WorkspaceID)
	}
	if m.Unmetered {
		attrs.PutBool(attrUnmetered, true)
	}
}

// stampAvailableTools computes the tracium.available_tools attribute from the
// upstream tool-definition and tool-call attributes. It is metadata (not raw
// content), so it is stamped regardless of the capture_content setting. No-op
// when the span offered no tools.
func stampAvailableTools(s ptrace.Span, attrs map[string]string) {
	if tools := genai.ToolsJSON(attrs); tools != "" {
		s.Attributes().PutStr(attrAvailableTools, tools)
	}
}

// stripContent removes raw prompt/completion content attributes (both the OTel
// and OpenLLMetry shapes, per genai.IsContentKey) so they never reach the
// exporter (and thus storage) when content capture is disabled. Tool-definition
// attributes are metadata and deliberately kept.
func stripContent(s ptrace.Span) {
	s.Attributes().RemoveIf(func(k string, _ pcommon.Value) bool {
		return genai.IsContentKey(k)
	})
}

// finishReason reads gen_ai.response.finish_reasons, which the OTel GenAI
// semantic conventions define as an *array*. We store the first element: a span
// describes a single model call, so anything past the first is degenerate, and
// a scalar is what equality filters (finish_reason = 'length') can match.
//
// Reading the value with AsString() instead would JSON-encode the array to
// `["length"]`, which no filter ever matches. Instrumentations that send a bare
// string are handled by the fallback.
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
