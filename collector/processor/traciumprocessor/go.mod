module github.com/tracium/collector/processor/traciumprocessor

go 1.26.0

// The Tracium domain logic (enrich, pricing, tenant, spanmodel) lives in the
// parent module and is consumed here behind the enrich.Enricher seam.
require github.com/tracium/collector v0.0.0

replace github.com/tracium/collector => ../../

// Collector framework deps are resolved at build time by the OCB builder.
// Versions MUST match the collector core version pinned in builder/*.builder.yaml.
require (
	go.opentelemetry.io/collector/client v1.69.0
	go.opentelemetry.io/collector/component v1.69.0
	go.opentelemetry.io/collector/consumer v1.69.0
	go.opentelemetry.io/collector/pdata v1.69.0
	go.opentelemetry.io/collector/processor v1.69.0
	go.uber.org/zap v1.28.0
)

require (
	go.opentelemetry.io/collector/processor/processorhelper v0.163.0
	go.opentelemetry.io/otel v1.47.0
	go.opentelemetry.io/otel/metric v1.47.0
)

require (
	github.com/ClickHouse/ch-go v0.74.0 // indirect
	github.com/ClickHouse/clickhouse-go/v2 v2.49.0 // indirect
	github.com/andybalholm/brotli v1.2.6 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-faster/city v1.0.1 // indirect
	github.com/go-faster/errors v0.7.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/go-version v1.9.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.19.1 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/paulmach/orb v0.13.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.27 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	go.opentelemetry.io/collector/featuregate v1.69.0 // indirect
	go.opentelemetry.io/collector/internal/componentalias v0.163.0 // indirect
	go.opentelemetry.io/collector/pipeline v1.69.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
)
