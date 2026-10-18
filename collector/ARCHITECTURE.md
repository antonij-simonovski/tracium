# Tracium Collector: Architecture

The collector is a **generic [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/)
distribution**, not a bespoke service. All the transport plumbing (OTLP
ingestion, batching, queueing, retry, back-pressure, health checks) comes from
upstream OTel components. Tracium adds three custom components:

| Component | Type | What it does |
|-----------|------|--------------|
| `processors.tracium` | processor | LLM-span enrichment: validate → normalise model → resolve user → compute cost → filter |
| `exporters.clickhousespan` | exporter | Writes the `tracium.spans` ClickHouse schema |
| `extensions.traciumauth` | extension | Verifies the per-workspace ingest API key on the OTLP receivers |

```
OTLP (gRPC/HTTP)  →  [otlp receiver]  →  [tracium processor]  →  [clickhousespan exporter]  →  ClickHouse
                        upstream            Tracium               Tracium
```

Batching happens inside the exporter, after its durable `sending_queue`
(`exporters.clickhousespan.batcher`), so a request is persisted before it is
acknowledged.

## The enrichment seam

**All domain logic is expressed as `enrich.Enricher` implementations** in the
framework-free [`enrich/`](enrich/) package:

```go
type Enricher interface {
    Enrich(ctx context.Context, span *spanmodel.Span) error
    Name() string
}
```

The processor registers [`enrich.DefaultChain`](enrich/enrichers.go): static
pricing, pass-through user, optional model allow-list. New behaviour is a new
`Enricher` added to that chain; the collector plumbing doesn't change.

The distribution is assembled by [`builder/oss.builder.yaml`](builder/oss.builder.yaml).

Because `enrich/` has **no dependency on the collector framework**, it compiles
and unit-tests without network access (`go test ./enrich/...`). The framework
adapters live in [`processor/traciumprocessor`](processor/traciumprocessor) and
[`exporter/clickhousespanexporter`](exporter/clickhousespanexporter), each its
own Go module (the standard OTel component layout).

## Module layout

```
collector/
├── enrich/                       # ← domain logic + enrichment seam (framework-free, tested)
├── internal/
│   ├── pricing/  user/           # resolvers used by the enrichers
│   ├── genai/  ingest/           # attribute parsing and ingest bounds
│   ├── deadletter/               # dead-letter store for rejected spans
│   ├── writer/                   # ClickHouse writer reused by the exporter
│   └── errors/                   # span/transient error taxonomy
├── pkg/spanmodel/                # the plain Span struct the chain operates on
├── processor/traciumprocessor/   # OTel processor adapter (own module)
├── exporter/clickhousespanexporter/  # OTel exporter adapter (own module)
├── extension/traciumauthextension/   # ingest authenticator (own module)
├── builder/                      # OCB manifest
├── config/collector.yaml         # generic collector runtime config
└── schema/                       # ClickHouse migrations
```

## Building

```bash
# Install the builder, pinned to the manifest's otelcol_version.
go install go.opentelemetry.io/collector/cmd/builder@v0.116.0

# Collector distribution → ./_build/collector
builder --config builder/oss.builder.yaml

# Run it
./_build/collector --config config/collector.yaml
```

Or via Docker: `docker build collector` (the [Dockerfile](Dockerfile) runs
OCB during the image build). Both require network access to download the
collector framework.

## What was removed

The previous bespoke collector hand-rolled the generic plumbing. Those packages
were deleted because upstream OTel now provides them:

| Removed | Replaced by |
|---------|-------------|
| `internal/receiver` (OTLP stubs) | `otlpreceiver` |
| `internal/pipeline` (retry/dead-letter) | exporter queue/retry + `enrich.Chain` |
| `internal/writer/buffer.go` | exporter `sending_queue` + `batcher` |
| `internal/health` | `healthcheckextension` |
| `internal/config`, `config.yaml`, `cmd/collector` | collector config + OCB binary |

The enrichment logic itself was preserved verbatim; it just moved from
pipeline `Stage`s into `enrich.Enricher`s.
