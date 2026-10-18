# collector

The ingest half of Tracium. It receives OTLP from your instrumented apps,
enriches each span with cost/user/workspace/normalised-model, and writes it to
ClickHouse. It does **not** serve queries; that's [`api`](../api).
It exposes no HTTP API of its own beyond a health check.

It is a **generic [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/)
distribution**, not a bespoke service. OTLP receiving, batching, the durable send
queue, retry, and health checks are all upstream OTel components. Tracium adds
exactly two:

- `processors.tracium`: the enrichment chain (validate → normalise model →
  resolve user → price → filter).
- `exporters.clickhousespan`: writes the `tracium.spans` schema.

All domain logic lives behind the `enrich.Enricher` seam in the framework-free
[`enrich/`](enrich/) package. The full component model is in
[`ARCHITECTURE.md`](ARCHITECTURE.md); the invariants you must not regress
(schema layout, ingest bounds, queue durability) are documented inline in
`schema/001_create_spans.sql` and `config/collector.yaml`.

## Build & run

The binary is assembled by the OpenTelemetry Collector Builder (OCB), pinned to
the manifest's collector version:

```bash
GOWORK=off go install go.opentelemetry.io/collector/cmd/builder@v0.116.0
GOWORK=off builder --config builder/oss.builder.yaml  # → ./_build/tracium-collector
./_build/tracium-collector --config config/collector.yaml
```

Or `docker build collector` from the repo root; the Dockerfile runs OCB
during the image build. Building always needs network access (OCB pulls the
collector framework); the `enrich/` core does not.

Point an OTLP exporter at `:4317` (gRPC) or `:4318` (HTTP). Every request must
carry a per-workspace API key (`Authorization: Bearer <trc_…>`); ingest is
key-only. `CLICKHOUSE_DSN` and `INGEST_VERIFY_URL` (the API's key-verify
endpoint) are required; see [`config/collector.yaml`](config/collector.yaml) for
every knob and its `${env:VAR}` override.

| Port | Purpose |
|---|---|
| 4317 | OTLP gRPC receiver |
| 4318 | OTLP HTTP/protobuf receiver |
| 8080 | Health / readiness (`healthcheckextension`) |
| 8888 | Collector's own Prometheus metrics (incl. span-drop counters) |

## Operational notes

- **The send queue is on disk and must stay on a persistent volume.** Spans
  already ACKed to the client wait there during a ClickHouse outage; a
  container-local path is wiped on restart and loses them. Compose uses a named
  volume, the Helm chart a per-replica PVC.
- **Dropped spans are never silent.** Invalid or filtered spans are dead-lettered
  with an error code and counted; see `internal/deadletter` and the `dropped`
  counter on `:8888`.
- **Ingest is bounded on purpose.** Even for an authenticated sender, token
  counts, model names, and user labels are capped before they can reach storage:
  one span claiming 2^62 tokens would otherwise poison every `sum(cost_usd)`.
  The ceilings live in [`enrich/enrichers.go`](enrich/enrichers.go).
- **Ingest requires a per-workspace API key.** The `traciumauth` authenticator is
  wired into both OTLP receivers and verifies every request's key against the API,
  rejecting unknown or revoked ones with 401. The key decides the workspace, so it
  overrides any sender-supplied `tracium.workspace.id`. As a fail-closed backstop,
  the `tracium` processor drops any span that reaches it without a verified key.
  This stops data *poisoning* and anonymous senders, not *volume*, so rate-limit at
  the gateway if the port is exposed.

## Test

```bash
go test ./enrich/...   # domain logic, no network needed
go test ./...          # includes the processor/exporter adapter modules
```

Unit tests use `testing/mocks` only, never a real database.

## License

Apache 2.0. See [LICENSE](LICENSE).
