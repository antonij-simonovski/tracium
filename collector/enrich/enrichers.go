package enrich

import (
	"context"
	"strings"

	customerrors "github.com/tracium/collector/internal/errors"
	"github.com/tracium/collector/internal/ingest"
	"github.com/tracium/collector/internal/pricing"
	"github.com/tracium/collector/internal/user"
	"github.com/tracium/collector/pkg/spanmodel"
)

// DefaultChain assembles the enrichment chain in the canonical order:
// validate → normalize model → resolve user → resolve pricing → filter.
//
// pricingResolver and userResolver may be nil; the corresponding step is then
// skipped (useful in local dev). allowedModels may be empty to allow all models.
func DefaultChain(
	pricingResolver pricing.Resolver,
	userResolver user.Resolver,
	allowedModels []string,
) *Chain {
	return NewChain(
		ValidateEnricher{},
		NormalizeModelEnricher{},
		UserEnricher{Resolver: userResolver},
		PricingEnricher{Resolver: pricingResolver},
		NewFilterEnricher(allowedModels),
	)
}

// ValidateEnricher fails fast on structurally invalid spans. It must run first.
type ValidateEnricher struct{}

// Name implements Enricher.
func (ValidateEnricher) Name() string { return "validate" }

// Enrich implements Enricher.
func (ValidateEnricher) Enrich(_ context.Context, span *spanmodel.Span) error {
	if span.TraceID == "" {
		return customerrors.InvalidSpan(customerrors.ErrMissingTraceID, "trace_id is empty")
	}
	if span.SpanID == "" {
		return customerrors.InvalidSpan(customerrors.ErrMissingSpanID, "span_id is empty")
	}
	if span.StartTimeMs <= 0 {
		return customerrors.InvalidSpanf(customerrors.ErrInvalidTimestamp,
			"start_time_ms must be positive, got %d", span.StartTimeMs)
	}
	// A span that ends before it starts has no trustworthy timing: its duration
	// is negative, which drags the enclosing trace's latency
	// (max(end) - min(start)) and every p95 below zero. Rejected rather than
	// clamped to 0, for two reasons: the clamp could not take effect anyway
	// (only the tracium.* attributes are written back to the OTLP span, and the
	// exporter recomputes duration from the original timestamps), and a clamped
	// span would silently report a plausible 0ms latency for a call whose clock
	// we know to be wrong. Dead-lettered with its code, never silently dropped.
	if span.EndTimeMs != 0 && span.EndTimeMs < span.StartTimeMs {
		return customerrors.InvalidSpanf(customerrors.ErrInvalidTimestamp,
			"end_time_ms %d precedes start_time_ms %d", span.EndTimeMs, span.StartTimeMs)
	}
	if len(span.Model) > ingest.MaxModelNameBytes {
		return customerrors.InvalidSpanf(customerrors.ErrFieldTooLong,
			"model name is %d bytes, limit is %d", len(span.Model), ingest.MaxModelNameBytes)
	}
	for _, tc := range []struct {
		field string
		count int64
	}{
		{"input_tokens", span.InputTokens},
		{"output_tokens", span.OutputTokens},
		{"cache_read_tokens", span.CacheReadTokens},
		{"cache_write_tokens", span.CacheWriteTokens},
	} {
		if tc.count > ingest.MaxTokensPerCall {
			return customerrors.InvalidSpanf(customerrors.ErrTokenCountOutOfRange,
				"%s is %d, limit is %d", tc.field, tc.count, ingest.MaxTokensPerCall)
		}
	}
	return nil
}

// NormalizeModelEnricher lower-cases, trims and sanitises the model name into
// ModelNormalized, which downstream pricing and filtering rely on — and which
// the daily rollup stores in a LowCardinality column.
type NormalizeModelEnricher struct{}

// Name implements Enricher.
func (NormalizeModelEnricher) Name() string { return "normalize-model" }

// Enrich implements Enricher.
func (NormalizeModelEnricher) Enrich(_ context.Context, span *spanmodel.Span) error {
	span.ModelNormalized = ingest.SanitizeIdentifier(strings.ToLower(span.Model), ingest.MaxModelNameBytes)
	if span.SchemaVersion == 0 {
		span.SchemaVersion = 1
	}
	return nil
}

// UserEnricher fills span.UserID via the configured Resolver. A nil Resolver
// leaves any existing UserID untouched (e.g. when tenancy is carried in OTLP
// resource attributes upstream).
type UserEnricher struct {
	Resolver user.Resolver
}

// Name implements Enricher.
func (UserEnricher) Name() string { return "user" }

// Enrich implements Enricher.
func (e UserEnricher) Enrich(ctx context.Context, span *spanmodel.Span) error {
	// The user label is client-controlled and reaches ClickHouse verbatim, so
	// it is sanitised whether or not a Resolver is configured.
	span.UserID = ingest.SanitizeIdentifier(span.UserID, ingest.MaxUserIDBytes)
	if e.Resolver == nil || span.UserID == "" {
		return nil
	}
	userID, err := e.Resolver.Resolve(ctx, span.UserID)
	if err != nil {
		return customerrors.TransientWrap(
			customerrors.ErrUserLookupFailed,
			"failed to resolve user",
			err,
		)
	}
	span.UserID = userID
	return nil
}

// PricingEnricher sets span.CostUSD from token usage via the Resolver. A missing
// price is non-fatal: the cost is recorded as 0 rather than dropping the span.
// A cost the instrumentation reported itself is never used: an ingest key
// authenticates the sender, not its numbers.
type PricingEnricher struct {
	Resolver pricing.Resolver
}

// Name implements Enricher.
func (PricingEnricher) Name() string { return "pricing" }

// Enrich implements Enricher.
func (e PricingEnricher) Enrich(ctx context.Context, span *spanmodel.Span) error {
	if e.Resolver == nil || span.ModelNormalized == "" {
		return nil
	}
	cost, err := e.Resolver.Resolve(ctx, span.ModelNormalized, pricing.Usage{
		Input:         span.InputTokens,
		Output:        span.OutputTokens,
		CacheRead:     span.CacheReadTokens,
		CacheCreation: span.CacheWriteTokens,
	})
	if err != nil {
		span.CostUSD = 0
		return nil
	}
	span.CostUSD = cost
	return nil
}

// FilterEnricher discards spans from models outside allowedModels. An empty
// allow-list permits every model.
type FilterEnricher struct {
	allowedModels map[string]bool
}

// NewFilterEnricher builds a FilterEnricher. Pass nil/empty to allow all models.
func NewFilterEnricher(models []string) FilterEnricher {
	m := make(map[string]bool, len(models))
	for _, name := range models {
		m[ingest.SanitizeIdentifier(strings.ToLower(name), ingest.MaxModelNameBytes)] = true
	}
	return FilterEnricher{allowedModels: m}
}

// Name implements Enricher.
func (FilterEnricher) Name() string { return "filter" }

// Enrich implements Enricher.
func (f FilterEnricher) Enrich(_ context.Context, span *spanmodel.Span) error {
	if len(f.allowedModels) == 0 {
		return nil
	}
	model := span.ModelNormalized
	if model == "" {
		model = span.Model
	}
	if !f.allowedModels[model] {
		return customerrors.InvalidSpanf(
			customerrors.ErrUnknownModel,
			"model %q is not in the allowed list",
			model,
		)
	}
	return nil
}
