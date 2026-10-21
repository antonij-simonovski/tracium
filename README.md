<p align="center">
  <img alt="Tracium robot mascot" src="docs/assets/tracium-robot-light.png#gh-light-mode-only" width="400">
  <img alt="Tracium robot mascot" src="docs/assets/tracium-robot-dark.png#gh-dark-mode-only" width="400">
</p>

<h3 align="center">Open-source, self-hosted LLM observability</h3>

<p align="center">
  Know what every model call costs, how long it takes, and when it fails,<br>
  per model, workflow, and end client. Built on OpenTelemetry.
</p>

<p align="center">
  <a href="LICENSE"><img alt="License: Apache 2.0" src="https://img.shields.io/badge/license-Apache%202.0-0e9f6e"></a>
  <a href="https://github.com/antonij-simonovski/tracium/releases"><img alt="Latest release" src="https://img.shields.io/github/v/release/antonij-simonovski/tracium?color=0e9f6e"></a>
  <a href="https://github.com/antonij-simonovski/tracium/stargazers"><img alt="GitHub stars" src="https://img.shields.io/badge/dynamic/json?url=https%3A%2F%2Fapi.github.com%2Frepos%2Fantonij-simonovski%2Ftracium&query=%24.stargazers_count&label=stars&color=0e9f6e"></a>
</p>

<p align="center">
  <a href="https://docs.tracium.ai">Docs</a> ·
  <a href="#quickstart">Quickstart</a> ·
  <a href="CONTRIBUTING.md">Contributing</a> ·
  <a href="CHANGELOG.md">Changelog</a>
</p>

![Tracium dashboard: overview, workflows, usage and clients](docs/assets/tracium-readme.gif)

## How it works

Tracium is an OpenTelemetry backend, not an SDK: anything that exports OTLP can
send to it. It reads the [GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/)
and [OpenLLMetry](https://github.com/traceloop/openllmetry) attributes, so OpenAI,
Anthropic, Bedrock, Vertex AI, LangChain, LlamaIndex and more work out of the box.

## Features

- **Anomaly detection**: spikes and drops in cost, error rate and runs, linked to the workflow version that likely caused them.
- **Workflow versions**: compare cost, latency and errors between releases of a prompt or agent.
- **Trace viewer**: every step of a run with its prompt, completion, tokens, cost and timing.
- **Cost allocation**: split spend by model, or by any attribute you attach (`team`, `environment`, …).
- **Clients**: meter spend and failures per end client.
- **MCP server**: ask Claude or any MCP client about your own cost, latency and failures; it can query any stored column, never through raw SQL.
- **Setup checks**: catch instrumentation gaps like unpriced models, with how to fix each.
- **Self-hosted**: one `docker compose up` or a Helm chart; your prompts and traces never leave your infrastructure.

## Quickstart

Want to try Tracium without setting up OpenTelemetry yourself? Give this prompt to your coding agent:

```text
Set up Tracium LLM observability for my project. Clone
https://github.com/antonij-simonovski/tracium, then read and follow AGENT_SETUP.md
from that clone. Don't change any code until I've told you which repositories to instrument.
```

Requires Docker with Compose v2.

```bash
git clone https://github.com/antonij-simonovski/tracium && cd tracium
cp .env.example .env   # set JWT_SECRET, CLICKHOUSE_PASSWORD, POSTGRES_PASSWORD (openssl rand -hex 32)
docker compose up
```

Open http://localhost:3000, create a workspace and an API key, then point your app at Tracium:

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_EXPORTER_OTLP_HEADERS="Authorization=Bearer YOUR_API_KEY"
```

No app yet? Run `cd dashboard && npm run seed` and sign in as `demo@tracium.ai` / `tracium-demo-1234`,
or try one of the [examples](examples/).

## Learn more

[Documentation](https://docs.tracium.ai) ·
[Configuration and development](CONTRIBUTING.md) ·
[Kubernetes](deploy/README.md) ·
[Upgrading](deploy/docs/upgrading.md) ·
[Security](SECURITY.md)

Apache 2.0. Contributions welcome, see [CONTRIBUTING.md](CONTRIBUTING.md).
