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
  <a href="LICENSE"><img alt="License: Apache 2.0" src="https://img.shields.io/badge/license-Apache%202.0-0e9f6e"></a>
  <a href="https://github.com/antonij-simonovski/tracium/releases"><img alt="Latest release" src="https://img.shields.io/github/v/release/antonij-simonovski/tracium?color=0e9f6e"></a>
  <a href="https://github.com/antonij-simonovski/tracium/stargazers"><img alt="GitHub stars" src="https://img.shields.io/github/stars/antonij-simonovski/tracium?color=0e9f6e"></a>
</p>

<p align="center">
  <a href="https://docs.tracium.ai">Docs</a> ·
  <a href="#quickstart">Quickstart</a> ·
  <a href="CONTRIBUTING.md">Contributing</a> ·
  <a href="CHANGELOG.md">Changelog</a>
</p>

![Tracium dashboard: overview, workflows, usage and clients](docs/assets/tracium-readme.gif)

## How it works

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/tracium-how-it-works-dark.svg">
  <img alt="Your app sends OTLP with an API key to the Tracium collector, which prices and tags each call and writes to ClickHouse; the dashboard reads it through the API. Everything runs on your infrastructure." src="docs/assets/tracium-how-it-works-light.svg" width="100%">
</picture>

Tracium is an OpenTelemetry backend, not an SDK: anything that exports OTLP can
send to it. It reads the [GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/)
and [OpenLLMetry](https://github.com/traceloop/openllmetry) attributes, so OpenAI,
Anthropic, Bedrock, Vertex AI, LangChain, LlamaIndex and more work out of the box.

## Features

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/tracium-features-dark.svg">
  <img alt="Tracium features: anomaly detection, workflow versions, trace viewer, cost allocation, per-client metering and setup checks." src="docs/assets/tracium-features-light.svg" width="100%">
</picture>

## Quickstart

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
