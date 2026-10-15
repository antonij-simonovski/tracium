<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/tracium-robot-dark.png">
    <img alt="Tracium robot mascot" src="docs/assets/tracium-robot-light.png" width="400">
  </picture>
</p>

<h3 align="center">Open-source, self-hosted LLM observability</h3>

<p align="center">
  Know what every model call costs, how long it takes, and when it fails,<br>
  per model, workflow, and end client. Built on OpenTelemetry.
</p>

<p align="center">
  <a href="https://docs.tracium.ai">Docs</a> ·
  <a href="#quickstart">Quickstart</a> ·
  <a href="CONTRIBUTING.md">Contributing</a> ·
  <a href="CHANGELOG.md">Changelog</a>
</p>

![Tracium dashboard: overview, workflows, usage and clients](docs/assets/tracium-readme.gif)

## Why Tracium

- **No SDK to adopt.** Tracium *is* an OpenTelemetry backend. Any app already
  exporting OTLP sends to it with three environment variables and no code changes.
- **Your data stays home.** One `docker compose up` runs the whole stack on your
  infrastructure; prompts and traces never leave it.
- **Fast at any size.** Daily rollups keep queries tied to the time window, not
  total rows, from the first span to hundreds of millions.
- **Apache 2.0.** Free to run, fork and extend.

## Features

- **Overview**: spend, runs, latency and error rate at a glance, with anomalies
  flagged and linked to the workflow version change that likely caused them.
- **Trace viewer**: every span of a run with its prompt, completion, tokens,
  cost and timing.
- **Workflows and versions**: compare cost, latency, errors and tokens between
  releases of a prompt or agent.
- **Usage and cost allocation**: break spend down by model, or by any attribute
  you attach (`team`, `environment`, …).
- **Clients**: meter spend, runs and failures per end client.
- **Setup checks**: catch instrumentation problems like unpriced models or
  missing token usage, with how to fix each.
- **Workspaces**: separate API keys and data per team, with member invites.
- **Runs anywhere**: Docker Compose locally, Helm on Kubernetes.

## Quickstart

Requires Docker with Compose v2.

```bash
git clone https://github.com/antonij-simonovski/tracium
cd tracium
cp .env.example .env
# Set JWT_SECRET, CLICKHOUSE_PASSWORD and POSTGRES_PASSWORD in .env,
# each to its own value from: openssl rand -hex 32
docker compose up
```

Open http://localhost:3000, sign up, create a workspace, and create an API key
under **API keys**. Then point your instrumented app at Tracium:

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_EXPORTER_OTLP_HEADERS="Authorization=Bearer YOUR_API_KEY"
```

Just want to look around? Seed ~1,500 demo traces with
`cd dashboard && npm run seed` and sign in as `demo@tracium.ai` / `tracium-demo-1234`.

## Works with

Tracium reads the [OpenTelemetry GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/)
and [OpenLLMetry](https://github.com/traceloop/openllmetry) attributes, so these
work out of the box:

- **OpenLLMetry** (Python, JS/TS): OpenAI, Anthropic, Bedrock, Vertex AI,
  LangChain, LlamaIndex and more.
- **OpenTelemetry GenAI instrumentations**, such as `opentelemetry-instrumentation-openai-v2`.
- **Your own spans** with `gen_ai.*` attributes. See [span setup](https://docs.tracium.ai/#otel-setup).

Runnable senders are in [`examples/`](examples/).

## Learn more

- [Documentation](https://docs.tracium.ai)
- [Architecture, configuration and development](CONTRIBUTING.md)
- [Kubernetes deployment](deploy/README.md)
- [Upgrading](deploy/docs/upgrading.md)

## Contributing

Contributions are welcome. Start with [CONTRIBUTING.md](CONTRIBUTING.md).
Security reports: [SECURITY.md](SECURITY.md).

## License

Apache 2.0. See [LICENSE](LICENSE).
