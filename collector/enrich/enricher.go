// Package enrich holds Tracium's domain-specific span enrichment logic,
// deliberately kept free of any OpenTelemetry Collector framework imports.
//
// The generic collector plumbing (receiving OTLP, batching, retry/queue,
// exporting) lives in upstream OTel components; the only Tracium-specific
// behaviour is expressed here as a chain of Enrichers that operate on the plain
// spanmodel.Span struct. DefaultChain (see enrichers.go) assembles the chain.
//
// Because nothing here depends on the collector framework, the whole package
// compiles and is unit-tested without network access; the framework adapter
// lives in processor/traciumprocessor.
package enrich

import (
	"context"

	"github.com/tracium/collector/pkg/spanmodel"
)

// Enricher is a single span-transformation step. Implementations must be safe
// for concurrent use, as the collector invokes them from multiple goroutines.
//
// A non-nil error drops the span. Return a *errors.SpanError or
// *errors.TransientError so the drop is dead-lettered with a code.
type Enricher interface {
	// Enrich transforms or validates the span in place.
	Enrich(ctx context.Context, span *spanmodel.Span) error
	// Name is a short, stable identifier used in logs and metrics.
	Name() string
}

// Chain runs an ordered list of Enrichers against a span.
type Chain struct {
	enrichers []Enricher
}

// NewChain builds a Chain from the given enrichers, run in the order supplied.
func NewChain(enrichers ...Enricher) *Chain {
	return &Chain{enrichers: enrichers}
}

// Apply runs every enricher in order and returns the first error, which means
// the span must be dropped.
func (c *Chain) Apply(ctx context.Context, span *spanmodel.Span) error {
	for _, e := range c.enrichers {
		if err := e.Enrich(ctx, span); err != nil {
			return err
		}
	}
	return nil
}
