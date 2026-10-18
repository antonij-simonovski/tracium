// Package clickhousespanexporter writes enriched Tracium LLM spans to the
// ClickHouse tracium.spans table.
package clickhousespanexporter

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterbatcher"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
)

var typeStr = component.MustNewType("clickhousespan")

// Config is set under exporters.clickhousespan in the collector config.
type Config struct {
	// DSN is the ClickHouse native DSN, e.g.
	// clickhouse://default:pass@clickhouse:9000/tracium
	DSN string `mapstructure:"dsn"`

	// QueueConfig and BackOffConfig are the standard exporterhelper
	// sending_queue / retry_on_failure blocks.
	QueueConfig   exporterhelper.QueueConfig `mapstructure:"sending_queue"`
	BackOffConfig configretry.BackOffConfig  `mapstructure:"retry_on_failure"`

	// BatchConfig batches spans after the durable sending_queue, so a request is
	// only acknowledged once it is persisted in the queue.
	BatchConfig exporterbatcher.Config `mapstructure:"batcher"`
}

// Validate implements component.ConfigValidator.
func (c *Config) Validate() error {
	if c.DSN == "" {
		return fmt.Errorf("clickhousespan: dsn must not be empty")
	}
	if err := c.QueueConfig.Validate(); err != nil {
		return fmt.Errorf("clickhousespan: sending_queue: %w", err)
	}
	if err := c.BackOffConfig.Validate(); err != nil {
		return fmt.Errorf("clickhousespan: retry_on_failure: %w", err)
	}
	if err := c.BatchConfig.Validate(); err != nil {
		return fmt.Errorf("clickhousespan: batcher: %w", err)
	}
	return nil
}

// NewFactory returns the factory for the ClickHouse span exporter. It writes
// both traces (real spans) and metrics (token-usage rows synthesised as
// source="metric" spans) into the same tracium.spans table.
func NewFactory() exporter.Factory {
	return exporter.NewFactory(
		typeStr,
		func() component.Config {
			return &Config{
				QueueConfig:   exporterhelper.NewDefaultQueueConfig(),
				BackOffConfig: configretry.NewDefaultBackOffConfig(),
				BatchConfig: exporterbatcher.Config{
					Enabled:       true,
					FlushTimeout:  5 * time.Second,
					MinSizeConfig: exporterbatcher.MinSizeConfig{MinSizeItems: 5000},
					MaxSizeConfig: exporterbatcher.MaxSizeConfig{MaxSizeItems: 10000},
				},
			}
		},
		exporter.WithTraces(createTracesExporter, component.StabilityLevelStable),
		exporter.WithMetrics(createMetricsExporter, component.StabilityLevelStable),
	)
}

func createTracesExporter(
	ctx context.Context,
	set exporter.Settings,
	cfg component.Config,
) (exporter.Traces, error) {
	c := cfg.(*Config)
	exp := &chExporter{dsn: c.DSN}
	return exporterhelper.NewTraces(
		ctx, set, cfg,
		exp.pushTraces,
		exporterhelper.WithStart(exp.start),
		exporterhelper.WithShutdown(exp.shutdown),
		exporterhelper.WithQueue(c.QueueConfig),
		exporterhelper.WithBatcher(c.BatchConfig),
		exporterhelper.WithRetry(c.BackOffConfig),
	)
}

func createMetricsExporter(
	ctx context.Context,
	set exporter.Settings,
	cfg component.Config,
) (exporter.Metrics, error) {
	c := cfg.(*Config)
	exp := &chExporter{dsn: c.DSN}
	return exporterhelper.NewMetrics(
		ctx, set, cfg,
		exp.pushMetrics,
		exporterhelper.WithStart(exp.start),
		exporterhelper.WithShutdown(exp.shutdown),
		exporterhelper.WithQueue(c.QueueConfig),
		exporterhelper.WithBatcher(c.BatchConfig),
		exporterhelper.WithRetry(c.BackOffConfig),
	)
}
