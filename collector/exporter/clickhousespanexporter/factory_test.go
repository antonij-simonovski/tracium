package clickhousespanexporter

import (
	"context"
	"testing"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/exporter"
)

// TestConfigAcceptsSendingQueue proves the sending_queue / retry_on_failure
// blocks in config/collector.yaml actually unmarshal into Config. The
// collector unmarshals component config with ErrorUnused, so before Config
// declared these fields any durable-queue config failed startup with an
// unknown-field error — the queue could only ever be the hardcoded in-memory
// default, and a restart during an outage destroyed spans already ACKed to
// the client.
func TestConfigAcceptsSendingQueue(t *testing.T) {
	conf := confmap.NewFromStringMap(map[string]any{
		"dsn": "clickhouse://tracium:pass@clickhouse:9000/tracium",
		"sending_queue": map[string]any{
			"storage": "file_storage",
			// Numeric, as the resolver produces: ${env:X:-200} expansion
			// re-parses the substituted scalar as YAML, so bare-number
			// defaults arrive typed, not as strings.
			"queue_size": 200,
		},
		"retry_on_failure": map[string]any{
			"enabled":          true,
			"max_elapsed_time": 0,
		},
	})

	cfg := NewFactory().CreateDefaultConfig().(*Config)
	if err := conf.Unmarshal(cfg); err != nil {
		t.Fatalf("collector.yaml-style exporter config does not unmarshal: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.QueueConfig.StorageID == nil || cfg.QueueConfig.StorageID.String() != "file_storage" {
		t.Fatalf("sending_queue.storage not honoured: %v", cfg.QueueConfig.StorageID)
	}
	if cfg.QueueConfig.QueueSize != 200 {
		t.Fatalf("queue_size = %d, want 200", cfg.QueueConfig.QueueSize)
	}
	if cfg.BackOffConfig.MaxElapsedTime != 0 {
		t.Fatalf("max_elapsed_time = %v, want 0 (retry indefinitely)", cfg.BackOffConfig.MaxElapsedTime)
	}
	// Fields the yaml does not set must keep the upstream defaults.
	if !cfg.QueueConfig.Enabled || cfg.QueueConfig.NumConsumers != 10 {
		t.Fatalf("unset fields lost their defaults: %+v", cfg.QueueConfig)
	}
}

// TestConfigAcceptsBatcher proves the batcher block in config/collector.yaml
// unmarshals into Config. Batching lives on the exporter (after the durable
// queue) rather than in a standalone `batch` processor (before it), so that a
// span is persisted before it is acknowledged.
func TestConfigAcceptsBatcher(t *testing.T) {
	conf := confmap.NewFromStringMap(map[string]any{
		"dsn": "clickhouse://tracium:pass@clickhouse:9000/tracium",
		"batcher": map[string]any{
			"enabled":        true,
			"flush_timeout":  "5s",
			"min_size_items": 5000,
			"max_size_items": 10000,
		},
	})

	cfg := NewFactory().CreateDefaultConfig().(*Config)
	if err := conf.Unmarshal(cfg); err != nil {
		t.Fatalf("collector.yaml-style batcher config does not unmarshal: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !cfg.BatchConfig.Enabled {
		t.Fatal("batcher.enabled not honoured")
	}
	if cfg.BatchConfig.MinSizeItems != 5000 || cfg.BatchConfig.MaxSizeItems != 10000 {
		t.Fatalf("batcher sizes = %d/%d, want 5000/10000", cfg.BatchConfig.MinSizeItems, cfg.BatchConfig.MaxSizeItems)
	}
}

// TestDefaultConfigBatchesByDefault pins that batching is on out of the box, so
// an operator who omits the batcher block still gets efficient inserts without
// reintroducing a pre-queue batch processor.
func TestDefaultConfigBatchesByDefault(t *testing.T) {
	cfg := NewFactory().CreateDefaultConfig().(*Config)
	if !cfg.BatchConfig.Enabled {
		t.Fatal("default config should batch on the exporter by default")
	}
}

// TestConfigRejectsUnknownField pins the ErrorUnused behaviour the fields
// above exist to satisfy: a mistyped key fails loudly instead of being
// silently dropped and leaving the operator with the settings they thought
// they had overridden.
func TestConfigRejectsUnknownField(t *testing.T) {
	conf := confmap.NewFromStringMap(map[string]any{
		"dsn":            "clickhouse://x",
		"sending_queues": map[string]any{"queue_size": 1},
	})
	cfg := NewFactory().CreateDefaultConfig().(*Config)
	if err := conf.Unmarshal(cfg); err == nil {
		t.Fatal("unknown top-level field was silently accepted")
	}
}

func TestClickHouseIsDialedInStartNotInFactory(t *testing.T) {
	ctx := context.Background()
	cfg := NewFactory().CreateDefaultConfig().(*Config)
	cfg.DSN = "clickhouse://127.0.0.1:1/tracium?dial_timeout=200ms"
	set := exporter.Settings{
		ID:                component.NewID(typeStr),
		TelemetrySettings: componenttest.NewNopTelemetrySettings(),
		BuildInfo:         component.NewDefaultBuildInfo(),
	}

	exp, err := NewFactory().CreateTraces(ctx, set, cfg)
	if err != nil {
		t.Fatalf("factory failed with ClickHouse unreachable: %v", err)
	}
	if err := exp.Start(ctx, componenttest.NewNopHost()); err == nil {
		t.Fatal("Start succeeded with ClickHouse unreachable")
	}
	if err := exp.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown after failed Start: %v", err)
	}
}
