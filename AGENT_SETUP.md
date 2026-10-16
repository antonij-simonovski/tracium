# Tracium setup for coding agents

You are a coding agent setting up Tracium, a self-hosted LLM observability
backend built on OpenTelemetry, for the user's project. Follow these steps in
order. Do not skip the questions: Tracium's features depend on answers only the
user can give.

Rules for the whole setup:

- Never remove, replace or reconfigure existing OpenTelemetry instrumentation.
  Tracium is added next to it.
- Never commit secrets. The ingest key goes wherever the repo already keeps
  secrets (`.env`, a secrets manager, deploy config), and `.env` files stay
  gitignored.
- Telemetry must never break the app. A failing exporter is logged, not raised.
- Batch your questions. Ask once per step, not once per detail.

## Step 1: Ask what to set up

Ask the user, in one message:

1. Which repositories or folders to instrument. Accept paths. If they are not
   sure, list the git repositories you can see and let them pick. Only touch
   the ones they name.
2. Where Tracium runs: start it locally with Docker (default), or use an
   existing Tracium URL they give you.

## Step 2: Start Tracium

Skip to the ingest key if the user gave you an existing Tracium. Ask them for
its OTLP ingest URL and its API URL, and use them wherever this guide says
`http://localhost:4318` and `http://localhost:8090`.

From the Tracium clone (Docker with Compose v2 required):

```bash
cp .env.example .env
```

Fill `JWT_SECRET`, `CLICKHOUSE_PASSWORD` and `POSTGRES_PASSWORD` in `.env` with
separate values from `openssl rand -hex 32`, then:

```bash
docker compose up -d
```

Wait until `curl -fs http://localhost:8090/v1/ready` succeeds.

The local endpoints, bound to 127.0.0.1:

| Service | URL |
|---|---|
| Dashboard | http://localhost:3000 |
| OTLP/HTTP ingest | http://localhost:4318 |
| OTLP/gRPC ingest | localhost:4317 |
| API | http://localhost:8090 |

An app running in a container reaches these at `host.docker.internal`, not
`localhost`. On Linux that does not work, because the ports are bound to
127.0.0.1. Attach the app's container to Tracium's Compose network instead
(find it with `docker network ls`, usually `tracium_default`) and send to
`http://collector:4318`.

**Ingest key.** Ask the user to open the dashboard, register, create a
workspace, and create an API key on the API keys screen, then give you the key
(`trc_...`). You cannot do this for them. Check the key:

```bash
curl -s -o /dev/null -w '%{http_code}\n' -X POST "${TRACIUM_OTLP_ENDPOINT:-http://localhost:4318}/v1/traces" \
  -H "Authorization: Bearer $TRACIUM_API_KEY" -H 'Content-Type: application/json' \
  -d '{"resourceSpans":[]}'
```

`200` means the key works. `401` means it is wrong or revoked.

All repositories share this one key. The key decides the workspace, so there is
no workspace attribute to set.

## Step 3: Survey each repository

Read only. For every repository the user named, find:

- Language, runtime and LLM libraries (OpenAI, Anthropic, Bedrock, Vertex AI,
  LangChain, LlamaIndex, ...), and whether calls are streamed.
- The entry points that start LLM work: HTTP handlers, jobs, CLI commands,
  queue consumers.
- Existing OpenTelemetry: SDK setup (`TracerProvider`, `MeterProvider`,
  `NodeSDK`, `Traceloop.init`, `configure_azure_monitor`, ...), `OTEL_*`
  environment variables, OpenTelemetry Collector configs, and vendor agents
  (Datadog, Sentry, Honeycomb, New Relic, Grafana, ...) that own the provider.
- Propagators in use, and how requests cross into the other named
  repositories (HTTP clients, gRPC, queues).
- How the repo manages secrets and environment variables, locally and in
  deploys.
- What could identify an end client (tenant, organization, account, API
  consumer) and any natural business dimensions (team, environment, feature).

Show the user a short table per repository with what you found and which
instrumentation path from step 5 you will take.

## Step 4: Settle the Tracium-specific features

Plain OpenTelemetry gives Tracium model, tokens, latency and errors. The
features below need values that OpenTelemetry does not define. Explain each one
to the user in a sentence, propose a value based on your survey, and ask them
to confirm or change it. Ask about all of them in one message.

| Feature | What it gives them | What to agree on |
|---|---|---|
| Workflow names | Cost, latency and errors per workflow instead of one bucket per service | Which entry points are separate workflows, and their names. Set as `gen_ai.agent.name` on each workflow's root span |
| Workflow versions | Compare releases, and anomalies linked to the version that caused them | The version source: git SHA, release tag or prompt version. It changes per release, never per run. Set as the `tracium.workflow.version` resource attribute |
| Clients | Spend and failures per end client | Which ID identifies an end client. Set as `tracium.user.id` |
| Cost allocation | Spend split by any attribute | Which attributes to add, such as `team`, `environment` or `feature` |
| Prompt capture | Prompts and completions in the trace viewer | Whether prompt and completion text may be stored, given PII and compliance |
| Token metrics | Cost totals that stay correct under trace sampling | On or off. On is recommended when traces are sampled |
| Pricing | Correct cost for every model | Fine-tuned, self-hosted or renamed models that need a price entry |

Rules behind these:

- `tracium.workflow.version` is read from the resource only. A span attribute
  is ignored.
- Put `tracium.user.id` on the resource when the process serves a single
  client, and on every span when it serves many. Token metrics only see
  resource attributes.
- Any attribute outside the `gen_ai.`, `tracium.`, `llm.` and `traceloop.`
  namespaces is stored for cost allocation. Span attributes win over resource
  attributes. Each span keeps at most 64 custom attributes, keys up to 128
  bytes and values up to 256 bytes.
- If `gen_ai.agent.name` is missing, the workflow name falls back to
  `traceloop.workflow.name`, `traceloop.entity.name`, `service.name`, and
  finally the span name.
- Prices come from `spec/pricing/pricing.json`, mounted into the collector. A
  model missing from it is recorded at $0. Add entries in the same shape as the
  existing ones and restart the collector.

## Step 5: Instrument each repository

Pick one path per repository.

**A. No OpenTelemetry yet.** Install OpenLLMetry (`traceloop-sdk` for Python,
`@traceloop/node-server-sdk` for Node) or the matching
`opentelemetry-instrumentation-*` packages for other languages. Export traces
over OTLP/HTTP to `<tracium>/v1/traces` with the header
`Authorization: Bearer <key>`, and metrics to `<tracium>/v1/metrics` if the user
wants them. See `examples/openai_to_tracium.py` for a complete Python setup.

**No GenAI instrumentation for the language** (common in Go, Java, Rust,
Ruby, PHP): wrap each LLM call in a span of kind CLIENT yourself, named
`chat <model>`, and set these attributes from the request and response:

| Attribute | Value |
|---|---|
| `gen_ai.operation.name` | `chat`, `embeddings`, ... |
| `gen_ai.request.model` | Model requested |
| `gen_ai.response.model` | Model the response names |
| `gen_ai.usage.input_tokens` | Input tokens from the response's usage |
| `gen_ai.usage.output_tokens` | Output tokens from the response's usage |
| `gen_ai.response.finish_reasons` | Finish reasons, as a string array |
| `gen_ai.is_streaming` | `true` for streamed calls |

Without the model and token counts the run has no cost.

**B. OpenTelemetry SDK already configured.** Keep the existing provider,
sampler, propagators, resource and exporters exactly as they are.

- Add a second span processor (a batch processor wrapping an OTLP/HTTP
  exporter pointed at Tracium) to the existing tracer provider.
- Add only the GenAI instrumentation libraries
  (`opentelemetry-instrumentation-openai`, `-anthropic`, `-langchain`, ...).
  Do not call `Traceloop.init` or any other one-call setup that creates its
  own provider.
- Merge new resource attributes into the existing resource. Do not replace it.
- For metrics, add a metric reader with an OTLP exporter to the existing meter
  provider.

**C. The app exports to an OpenTelemetry Collector.** Leave the app's exporter
alone. Add an `otlphttp` exporter to the Collector config with
`endpoint: <tracium>` and the `Authorization` header, and add it to the traces
pipeline, and the metrics pipeline if wanted, next to the existing exporters.
GenAI instrumentation may still be missing in the app; add it as in path B.

**D. A vendor agent owns the provider** (Datadog, New Relic, Sentry, ...).
Check whether it exposes the OpenTelemetry provider so you can add a span
processor. If not, stop and ask the user before changing anything.

On every path:

- Do not set or change the generic `OTEL_EXPORTER_OTLP_*` variables if
  something already uses them. Configure the Tracium exporter in code from its
  own variables (`TRACIUM_OTLP_ENDPOINT`, `TRACIUM_API_KEY`), and add them to
  the repo's env example file and deploy config placeholders.
- Set a meaningful `service.name` if none exists. It must differ between
  repositories.
- Apply the values agreed in step 4. Set `gen_ai.agent.name` on the root span
  of each workflow, wrapping the entry point in a span if there is none.
- Export metrics with DELTA temporality, set in the exporter's options, not
  with a global environment variable.
- Streaming: make the call return token usage (OpenAI:
  `stream_options={"include_usage": True}`), and end the span only after the
  stream is consumed.
- Failures: record the exception on the span and set an error status with a
  message.
- Prompt capture off: OpenLLMetry `TRACELOOP_TRACE_CONTENT=false`. The
  `opentelemetry-instrumentation-*-v2` packages capture nothing unless
  `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT=true`.
- Flush the providers on shutdown in short-lived processes (scripts, jobs,
  serverless).

## Step 6: Connect multiple repositories

Skip if the user named one repository.

- Use the same ingest key and the same version scheme in every repository.
- Make sure trace context crosses every boundary between them, so one request
  is one trace. Use W3C `traceparent` propagation on HTTP and gRPC clients and
  servers, and inject and extract it in queue message headers. Add the
  propagator only if none is configured; never replace an existing one.
- Every service on the path must export to Tracium. Otherwise traces arrive
  without their root span and lose their workflow name.

## Step 7: Verify

For each repository, note the time, then run the app, a test, or a small
script that makes one real LLM call through an instrumented path. Wait a few
seconds, then read the setup checks for spans since that time with the ingest
key:

```bash
SINCE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
# run the instrumented call here, then:
curl -s -H "Authorization: Bearer $TRACIUM_API_KEY" \
  "${TRACIUM_API_URL:-http://localhost:8090}/v1/metrics/setup-checks?range=24h&since=$SINCE"
```

`items` lists each problem with its `code`, `severity`, `message` and an
`example_trace_id`. An empty list means the checks are clear. The key reads
only this endpoint, so ask the user to open the dashboard and confirm the run
appears under the right workflow name and version, with a cost, not $0. An
empty list before the run shows up there only means nothing arrived yet.

Fix what the checks and the user report, then check again with a new `SINCE`.
The checks name the problem and the fix: no LLM spans,
unpriced model, missing model, unmetered streams, instant LLM spans, errors
without detail, missing root span, unnamed workflow, workflow without version,
version per run, and spans rejected at ingest.

## Step 8: Report

End with a report to the user, one section per repository:

- **Path taken**: A, B, C or D, and why.
- **Existing instrumentation**: what you found and confirm it is unchanged.
- **Files changed** and **dependencies added**.
- **Tracium attributes**: each one set, its value or source, and where it is
  set.
- **Environment variables**: names added, where they need values (local,
  CI, each deploy target).
- **Verification**: what you ran and what showed up in Tracium.
- **Open items**: anything the user must still do, such as deploy secrets,
  price entries, services that do not export yet, and setup checks still
  showing.

Do not commit or push unless the user asks.
