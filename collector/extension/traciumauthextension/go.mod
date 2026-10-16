module github.com/tracium/collector/extension/traciumauthextension

go 1.26.0

// Collector framework deps are resolved at build time by the OCB builder.
// Versions MUST match the collector core version pinned in builder/*.builder.yaml
// (and the sibling traciumprocessor module), so the assembled distribution keeps
// a single, consistent module graph.
require (
	go.opentelemetry.io/collector/client v1.22.0
	go.opentelemetry.io/collector/component v1.68.0
	go.opentelemetry.io/collector/extension v1.68.0
	go.opentelemetry.io/collector/extension/auth v0.116.0
	go.uber.org/zap v1.28.0
)

require google.golang.org/grpc v1.83.2

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/hashicorp/go-version v1.9.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	go.opentelemetry.io/collector/featuregate v1.68.0 // indirect
	go.opentelemetry.io/collector/internal/componentalias v0.162.0 // indirect
	go.opentelemetry.io/collector/pdata v1.68.0 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/trace v1.46.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)
