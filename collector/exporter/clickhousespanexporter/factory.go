// Package clickhousespanexporter writes enriched Tracium LLM spans to the
// ClickHouse tracium.spans table.
package clickhousespanexporter

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
)

var typeStr = component.MustNewType("clickhousespan")

// Config is set under exporters.clickhousespan in the collector config.
type Config struct {
	// DSN is the ClickHouse native DSN, e.g.
	// clickhouse://default:pass@clickhouse:9000/tracium
	DSN string `mapstructure:"dsn"`

	// QueueConfig and BackOffConfig are the standard exporterhelper
	// sending_queue / retry_on_failure blocks. sending_queue.batch batches spans
	// after the durable queue, so a request is only acknowledged once it is
	// persisted in the queue.
	QueueConfig   configoptional.Optional[exporterhelper.QueueBatchConfig] `mapstructure:"sending_queue"`
	BackOffConfig configretry.BackOffConfig                                `mapstructure:"retry_on_failure"`
}

// Validate implements component.ConfigValidator.
func (c *Config) Validate() error {
	if c.DSN == "" {
		return fmt.Errorf("clickhousespan: dsn must not be empty")
	}
	if q := c.QueueConfig.Get(); q != nil {
		if err := q.Validate(); err != nil {
			return fmt.Errorf("clickhousespan: sending_queue: %w", err)
		}
		if b := q.Batch.Get(); b != nil {
			if err := b.Validate(); err != nil {
				return fmt.Errorf("clickhousespan: sending_queue.batch: %w", err)
			}
		}
	}
	if err := c.BackOffConfig.Validate(); err != nil {
		return fmt.Errorf("clickhousespan: retry_on_failure: %w", err)
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
			batch := exporterhelper.NewDefaultBatchConfig()
			batch.FlushTimeout = 5 * time.Second
			batch.Sizer = exporterhelper.RequestSizerTypeItems
			batch.MinSize = 5000
			batch.MaxSize = 10000
			queue := exporterhelper.NewDefaultQueueConfig()
			queue.Batch = configoptional.Some(batch)
			return &Config{
				QueueConfig:   configoptional.Some(queue),
				BackOffConfig: configretry.NewDefaultBackOffConfig(),
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
		exporterhelper.WithRetry(c.BackOffConfig),
	)
}
