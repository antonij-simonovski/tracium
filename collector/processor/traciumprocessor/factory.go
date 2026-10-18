// Package traciumprocessor is the OpenTelemetry Collector processor that applies
// Tracium's LLM-span enrichment (validation, model normalisation, user
// resolution, per-span cost, model filtering).
//
// It is a thin adapter: all domain behaviour lives in the framework-free
// github.com/tracium/collector/enrich package behind the Enricher seam.
package traciumprocessor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/tracium/collector/enrich"
	"github.com/tracium/collector/internal/deadletter"
	"github.com/tracium/collector/internal/pricing"
	"github.com/tracium/collector/internal/user"
	"github.com/tracium/collector/internal/writer"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processorhelper"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// typeStr is the component type used in collector config (processors.tracium).
var typeStr = component.MustNewType("tracium")

// NewFactory returns the factory for the Tracium processor. It handles both
// traces (per-span enrichment) and metrics (token-usage → cost), so a single
// `processors.tracium` entry can sit in both pipelines.
func NewFactory() processor.Factory {
	return processor.NewFactory(
		typeStr,
		createDefaultConfig,
		processor.WithTraces(createTracesProcessor, component.StabilityLevelStable),
		processor.WithMetrics(createMetricsProcessor, component.StabilityLevelStable),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		Pricing: PricingConfig{Source: "static"},
		User:    UserConfig{Source: "passthrough"},
	}
}

// buildResolvers constructs the pricing and user resolvers from config.
// Both the traces enrichment chain and the metrics processor share them, so a
// single price table and user policy govern spans and metric-derived rows
// alike.
func buildResolvers(cfg *Config, logger *zap.Logger) (pricing.Resolver, user.Resolver, error) {
	// Pricing source: static.
	prices := pricing.DefaultPrices()
	if cfg.Pricing.StaticFilePath != "" {
		loaded, err := pricing.LoadStaticFile(cfg.Pricing.StaticFilePath)
		switch {
		case err == nil:
			prices = loaded
		case errors.Is(err, fs.ErrNotExist):
			// Absent file is a valid "use built-in defaults" signal (local dev).
			logger.Info("pricing file not found, using built-in defaults",
				zap.String("path", cfg.Pricing.StaticFilePath))
		default:
			// A present-but-unreadable file is a misconfiguration: surfacing it
			// beats silently shipping every span with zero cost.
			return nil, nil, fmt.Errorf("load pricing file: %w", err)
		}
	}
	pricingResolver := pricing.NewStaticResolver(prices)

	var userResolver user.Resolver
	if cfg.User.Source == "passthrough" || cfg.User.Source == "" {
		userResolver = user.Passthrough{}
	}

	return pricingResolver, userResolver, nil
}

// buildChain turns the validated Config into the enrichment chain.
func buildChain(cfg *Config, logger *zap.Logger) (*enrich.Chain, error) {
	pricingResolver, userResolver, err := buildResolvers(cfg, logger)
	if err != nil {
		return nil, err
	}
	return enrich.DefaultChain(pricingResolver, userResolver, cfg.AllowedModels), nil
}

func createTracesProcessor(
	ctx context.Context,
	set processor.Settings,
	cfg component.Config,
	next consumer.Traces,
) (processor.Traces, error) {
	pCfg := cfg.(*Config)
	chain, err := buildChain(pCfg, set.TelemetrySettings.Logger)
	if err != nil {
		return nil, err
	}
	dlq, err := buildDeadLetter(pCfg, set.TelemetrySettings.Logger)
	if err != nil {
		return nil, err
	}
	dropped, err := droppedSpansCounter(set)
	if err != nil {
		return nil, err
	}
	tp := &traciumProcessor{
		logger:         set.TelemetrySettings.Logger,
		chain:          chain,
		captureContent: pCfg.CaptureContent,
		deadLetter:     dlq,
		dropped:        dropped,
	}
	return processorhelper.NewTraces(
		ctx, set, cfg, next,
		tp.processTraces,
		processorhelper.WithCapabilities(consumer.Capabilities{MutatesData: true}),
		processorhelper.WithStart(func(ctx context.Context, _ component.Host) error {
			return tp.startTally(ctx, pCfg.DeadLetter.ClickHouseDSN)
		}),
		processorhelper.WithShutdown(func(context.Context) error { return tp.deadLetter.Close() }),
	)
}

const rejectedFlushInterval = 10 * time.Second

// buildDeadLetter selects the dead-letter store: an NDJSON file when the
// operator configured a path (drops are then recoverable), otherwise the log
// store, which keeps every drop visible with zero configuration.
func buildDeadLetter(cfg *Config, logger *zap.Logger) (deadletter.Store, error) {
	if path := cfg.DeadLetter.Path; path != "" {
		return deadletter.NewFileStore(path)
	}
	return deadletter.NewLogStore(zapDeadLetterLogger{logger}), nil
}

// startTally connects to ClickHouse and starts counting rejections per
// workspace when a DSN is configured.
func (p *traciumProcessor) startTally(ctx context.Context, dsn string) error {
	if dsn == "" {
		return nil
	}
	w, err := writer.NewClickHouseWriter(ctx, dsn)
	if err != nil {
		return err
	}
	p.deadLetter = deadletter.NewTallyStore(p.deadLetter, w, zapDeadLetterLogger{p.logger}, rejectedFlushInterval)
	return nil
}

// zapDeadLetterLogger adapts the collector's zap logger to deadletter.Logger,
// so dead-letter records honour the collector's configured level and format
// instead of going out through a second, separately-configured logger.
type zapDeadLetterLogger struct{ l *zap.Logger }

// Warn implements deadletter.Logger.
func (z zapDeadLetterLogger) Warn(msg string, args ...any) { z.l.Sugar().Warnw(msg, args...) }

// droppedSpansCounter builds the counter for permanently rejected spans. The
// processor mutates the batch in place, so processorhelper reports every span
// as accepted — without this counter a drop is invisible to monitoring.
func droppedSpansCounter(set processor.Settings) (metric.Int64Counter, error) {
	if set.TelemetrySettings.MeterProvider == nil {
		return nil, nil
	}
	return set.TelemetrySettings.MeterProvider.
		Meter("github.com/tracium/collector/processor/traciumprocessor").
		Int64Counter(
			"tracium_processor_spans_dropped",
			metric.WithDescription("Spans permanently rejected by the enrichment chain, by error code."),
			metric.WithUnit("{span}"),
		)
}

func createMetricsProcessor(
	ctx context.Context,
	set processor.Settings,
	cfg component.Config,
	next consumer.Metrics,
) (processor.Metrics, error) {
	pCfg := cfg.(*Config)
	pricingResolver, userResolver, err := buildResolvers(pCfg, set.TelemetrySettings.Logger)
	if err != nil {
		return nil, err
	}
	mp := &metricsProcessor{
		logger:  set.TelemetrySettings.Logger,
		pricing: pricingResolver,
		user:    userResolver,
	}
	return processorhelper.NewMetrics(
		ctx, set, cfg, next,
		mp.processMetrics,
		processorhelper.WithCapabilities(consumer.Capabilities{MutatesData: true}),
	)
}
