module github.com/tracium/collector/extension/traciumauthextension

go 1.25.0

// Collector framework deps are resolved at build time by the OCB builder.
// Versions MUST match the collector core version pinned in builder/*.builder.yaml
// (and the sibling traciumprocessor module), so the assembled distribution keeps
// a single, consistent module graph.
require (
	go.opentelemetry.io/collector/client v1.22.0
	go.opentelemetry.io/collector/component v0.116.0
	go.opentelemetry.io/collector/extension v0.116.0
	go.opentelemetry.io/collector/extension/auth v0.116.0
	go.uber.org/zap v1.28.0
)

require google.golang.org/grpc v1.84.0

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	go.opentelemetry.io/collector/config/configtelemetry v0.116.0 // indirect
	go.opentelemetry.io/collector/pdata v1.22.0 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)
