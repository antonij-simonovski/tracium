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
- Telemetry must never break the app. A failing exporter is logged, not raised,
  and missing Tracium settings turn telemetry off instead of failing start-up
  (watch for env helpers that throw on a missing variable).
- Batch your questions. Ask once per step, not once per detail.
- Projects differ. The defaults below are starting points, not answers. Read
  the code first and work out what fits; ask only what the code cannot tell
  you. When more than one option is reasonable, describe each in a sentence,
  recommend one, and ask. Re-ask when the user's answer and the code disagree.

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

If the user ran Tracium before, Docker volumes from that install may exist
under the same Compose project name, with other passwords, and the migrations
fail to authenticate. Do not delete them without asking: set
`COMPOSE_PROJECT_NAME` in `.env` to a new name instead, or ask whether the old
data can go.

Wait until `curl -fs http://localhost:8090/v1/ready` succeeds.

The local endpoints, bound to 127.0.0.1:

| Service | URL |
|---|---|
| Dashboard | http://localhost:3000 |
| OTLP/HTTP ingest | http://localhost:4318 |
| OTLP/gRPC ingest | localhost:4317 |
| API | http://localhost:8090 |

An app running in a container reaches these at `host.docker.internal`, not
`localhost`. This includes functions run by a local cloud emulator
(LocalStack, ministack, SAM, Functions emulators), which execute in containers
even when the rest of the app runs on the host. On Linux `host.docker.internal`
does not work, because the ports are bound to 127.0.0.1. Attach the app's
container to Tracium's Compose network instead (find it with
`docker network ls`) and send to `http://collector:4318`.

**Ingest key.** On the local Docker install, ask the user for the email to
sign in with, then create the account, a workspace and a key in one step:

```bash
docker compose exec -T api ./bootstrap --email <their email>
```

It prints the key (`trc_...`) on stdout. For a new account it also prints a
generated password on stderr: give it to the user for the dashboard, in a file
or message they can read but outside any repository, and tell them to change it
in the dashboard settings. An existing account keeps its password and its first
workspace; each run adds a new key.

On an existing or hosted Tracium, ask the user to open the dashboard, create a
workspace if they have none, and create an API key on the API keys screen, then
give you the key. You cannot do this for them.

Check the key:

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
  Gemini, LangChain, LlamaIndex, pydantic-ai, ...), whether calls are streamed,
  and which higher-level libraries call which SDK underneath (instrumenting
  both layers double-counts tokens and cost).
- Other paid AI services the code calls: OCR (Textract, Document AI, Azure
  Document Intelligence), speech, translation, image models, hosted
  embeddings. Note how each one bills (pages, characters, seconds, images).
- The entry points that start LLM work: HTTP handlers, jobs, CLI commands,
  queue consumers, scheduled functions.
- How work flows across services: orchestrators or workflow engines, queues,
  task workers, fan-out and fan-in, callbacks, and processes that pause and are
  resumed later. Note where one business process starts and ends, and whether
  one user action spans several services or functions. Include small worker
  functions between the main services; they are easy to miss.
- Existing OpenTelemetry: SDK setup (`TracerProvider`, `MeterProvider`,
  `NodeSDK`, `Traceloop.init`, `configure_azure_monitor`, ...), `OTEL_*`
  environment variables, OpenTelemetry Collector configs, and vendor agents
  (Datadog, Sentry, Honeycomb, New Relic, Grafana, ...) that own the provider.
- Propagators in use, and how requests cross between the named repositories
  (HTTP clients, gRPC, queues, direct function invocation).
- How the repo manages secrets and environment variables, locally and in
  deploys, including how local emulators or containers get theirs.
- What could identify an end client (tenant, organization, account, API
  consumer) and any natural business dimensions (team, environment, feature).

Show the user a short table per repository with what you found and which
instrumentation path from step 5 you will take, and a short description of how
a request or process flows across the repositories.

## Step 4: Settle the Tracium-specific features

Plain OpenTelemetry gives Tracium model, tokens, latency and errors. The
features below need values that OpenTelemetry does not define, or choices only
the user can make. Explain each one to the user in a sentence, propose a value
based on your survey (with the alternatives when the project allows several),
and ask them to confirm or change it. Ask about all of them in one message.

| Feature | What it gives them | What to agree on |
|---|---|---|
| Workflows | Cost, latency and errors per workflow instead of one bucket per service | What one run is, and its name, proposed from the code. See "Choosing workflows" below |
| Trace scope | What the trace viewer shows | One trace per request, per job, or per whole business process; and whether Tracium receives full traces or only GenAI spans (plus other paid AI services). See "Choosing trace scope" |
| Workflow versions | Compare releases, and anomalies linked to the version that caused them | The version source: git SHA, release tag, prompt version, or a value developers set by hand. It changes per release, never per run, and may differ per service. Set as the `tracium.workflow.version` resource attribute |
| Clients | Spend and failures per end client | Which ID identifies an end client, per repository. Set as `tracium.user.id` |
| Cost allocation | Spend split by any attribute | Which attributes to add, such as `team`, `environment` or `feature` |
| Prompt capture | Prompts and completions in the trace viewer | Whether prompt and completion text may be stored, given PII and compliance, and in which environments (for example local only) |
| Token metrics | Cost totals that stay correct under trace sampling | On or off. On is recommended when traces are sampled |
| Pricing | Correct cost for every model | Fine-tuned, self-hosted or renamed models that need a price entry |
| Other paid AI services | Visibility into non-LLM spend | Whether to trace them, and what to record. See "Non-LLM services" |

### Choosing workflows

Tracium reads the workflow from each span: `gen_ai.agent.name`, then
`traceloop.workflow.name`, `traceloop.entity.name`, `service.name`, and finally
the span name. Every span is filed under its own workflow, so spans without
`gen_ai.agent.name` land under their service's name. Common shapes:

- **One workflow per entry point** (a chat endpoint, a nightly job): set
  `gen_ai.agent.name` on the entry point's root span and on its child spans,
  or rely on a per-service `service.name` when a service does one thing.
- **One workflow per business process across services** (an orchestrator or
  pipeline that calls several functions or services for one user action): the
  workflow name is decided where the process starts and must reach every span
  of the process, in every service. Carry it as W3C baggage next to
  `traceparent` and stamp it as `gen_ai.agent.name` on every span with a span
  processor. Name it after the process (its type or blueprint), not after the
  orchestrator or any one service; a process family can share one name, with
  the exact variant as an extra attribute.
- **One workflow per service or function**: simplest, but a process that spans
  services is then split across workflows. Confirm this is what the user wants
  when the survey shows a pipeline.

Work out the shape from the code before asking: where requests and jobs
enter, whether an orchestrator or queue chain ties several services into one
process, and what the code itself calls those processes (process types,
blueprints, job names). Propose workflow names from that. Ask only when the
code supports more than one reading.

### Choosing trace scope

- A trace is as long as context propagation makes it. One trace per business
  process needs context carried across every hop (step 6). Processes that are
  resumed later by a new user action usually start a new trace unless the
  context is persisted; ask whether that is acceptable.
- Some users want Tracium to receive only GenAI spans (and other paid AI
  calls), not HTTP, database, storage or queue spans. Do this by filtering at
  export (a span processor or exporter wrapper that forwards only spans with
  `gen_ai.request.model`, plus the agreed services), not by skipping the other
  spans: the unexported spans still carry trace context and baggage to their
  children. Tell the user the cost of this: traces arrive without their root
  span (the `missing_root_span` check), and run latency covers only the
  exported calls.

### Non-LLM services

Tracium prices spans from model and token counts only, and it ignores cost
reported by the sender. A non-LLM service (OCR, speech, translation) can be
traced with latency, errors and its billing unit as a custom attribute (for
example `textract.pages`), which is stored for cost allocation, but its cost
shows as $0. Say so plainly. Do not invent a price, for example by passing the
service off as a priced model, unless the user explicitly asks for that and
understands it pollutes the LLM statistics.

Rules behind these:

- `tracium.workflow.version` is read from the resource only. A span attribute
  is ignored.
- Put `tracium.user.id` on the resource when the process serves a single
  client, and on every span when it serves many (for example, a queue consumer
  handling a batch from several clients). Token metrics only see resource
  attributes.
- Any attribute outside the `gen_ai.`, `tracium.`, `llm.` and `traceloop.`
  namespaces is stored for cost allocation. Span attributes win over resource
  attributes. Each span keeps at most 64 custom attributes, keys up to 128
  bytes and values up to 256 bytes.
- Prices come from `spec/pricing/pricing.json`, mounted into the collector. A
  model missing from it is recorded at $0. Check every model the code uses,
  including defaults read from environment variables, against it. Add entries
  in the same shape as the existing ones and restart the collector.

## Step 5: Instrument each repository

Pick one path per repository.

**A. No OpenTelemetry yet.** Install OpenLLMetry (`traceloop-sdk` for Python,
`@traceloop/node-server-sdk` for Node) or the matching
`opentelemetry-instrumentation-*` packages for other languages. Export traces
over OTLP/HTTP to `<tracium>/v1/traces` with the header
`Authorization: Bearer <key>`, and metrics to `<tracium>/v1/metrics` if the user
wants them. See `examples/openai_to_tracium.py` for a complete Python setup.
When a framework calls an SDK underneath (pydantic-ai or LangChain over a
provider SDK), instrument one layer only; prefer the layer that sees every call
in the codebase.

**No GenAI instrumentation for the language or library** (common in Go, Java,
Rust, Ruby, PHP, and for SDKs whose instrumentation does not work with the
project's module system or bundler): wrap each LLM call in a span of kind
CLIENT yourself, named `chat <model>`, and set these attributes from the request
and response:

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
  provider. If the existing meter provider exists only in some environments,
  say so rather than creating a second one silently.
- If the provider traces more than LLM work (HTTP requests, database calls,
  queues), send Tracium only the traces that contain an LLM call. Otherwise
  every request becomes a run and buries the LLM workflows. Put a filter in
  front of the Tracium exporter only, never the existing ones. It holds each
  trace's spans in memory until the trace's local root ends (the span with no
  parent, or a remote one), then exports all of them if any span has a
  `gen_ai.*`, `llm.*` or `traceloop.*` attribute, and drops them otherwise.
  Holding the whole trace keeps tool calls, retrieval and embeddings that run
  before the first LLM call. Bound the buffer by traces and age; when it
  overflows, export the evicted trace if it has an LLM span and let that
  trace's later spans pass straight through, so nothing that matters is lost.
- The existing sampler decides for Tracium too. If it samples, tell the user
  that Tracium sees the same share of traces, and recommend token metrics so
  cost totals stay complete.

**C. The app exports to an OpenTelemetry Collector.** Leave the app's exporter
alone. Add an `otlphttp` exporter to the Collector config with
`endpoint: <tracium>` and the `Authorization` header, and add it to the traces
pipeline, and the metrics pipeline if wanted, next to the existing exporters.
GenAI instrumentation may still be missing in the app; add it as in path B.

**D. A vendor agent owns the provider** (Datadog, New Relic, Sentry, ...).
Check whether it exposes the OpenTelemetry provider so you can add a span
processor, then follow path B, including the LLM-only filter: these agents
trace every request. If it does not expose one, stop and ask the user before
changing anything. Never change what the vendor receives: its resource,
sampling, propagation and exported data stay exactly as they were.

On every path:

- Do not set or change the generic `OTEL_EXPORTER_OTLP_*` variables if
  something already uses them. Configure the Tracium exporter in code from its
  own variables (`TRACIUM_OTLP_ENDPOINT`, `TRACIUM_API_KEY`), and add them to
  the repo's env example file and deploy config placeholders.
- Set a meaningful `service.name` if none exists. It must differ between
  repositories and between independently deployed functions.
- Apply the values agreed in step 4, including the workflow and trace scope
  choices. Wrap entry points in a span when there is none.
- Export metrics with DELTA temporality, set in the exporter's options, not
  with a global environment variable. Tracium rejects cumulative metrics.
- Streaming: make the call return token usage (OpenAI:
  `stream_options={"include_usage": True}`; Anthropic: the final message), and
  end the span only after the stream is consumed.
- Failures: record the exception on the span and set an error status with a
  message, including for errors the code catches and handles.
- Prompt capture off: OpenLLMetry `TRACELOOP_TRACE_CONTENT=false`. The
  `opentelemetry-instrumentation-*-v2` and `-google-genai` packages capture
  nothing unless `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT` is set.
  When capture is allowed only in some environments, decide it in code from
  the environment, not from a variable that could leak into deploys.
- Flush the providers before short-lived processes return (scripts, jobs,
  serverless invocations), on every invocation, not only on shutdown.
- New dependencies follow the repository's rules (exact pins, audits). Check
  that the packages you add do not bring in known high-severity advisories;
  pick a narrower package when one does.
- Local tooling counts as config: if functions run in a local emulator or
  containers, the scripts that deploy them locally must pass the Tracium
  variables too, with an endpoint the container can reach (step 2).

## Step 6: Connect multiple services

Skip if there is only one service and no queues.

- Use the same ingest key in every repository, and the version scheme agreed
  in step 4.
- Make sure trace context crosses every boundary that belongs to one run, so
  one run is one trace. Use W3C `traceparent` propagation on HTTP and gRPC
  clients and servers, and inject and extract it in queue message attributes
  and direct function-invocation payloads. Add the propagator only if none is
  configured; never replace an existing one.
- Every hop of a run needs both halves, injection in the sender and
  extraction in the receiver; one missing hop splits the trace. Check that
  instrumentation libraries really propagate through the transports in use
  rather than assuming it, and confirm it end to end in step 7.
- When the workflow is a cross-service process, carry it as W3C baggage with
  the trace context.
- Decide per boundary whether the receiver continues the sender's workflow or
  starts its own, from the workflows agreed in step 4:
  - **Same workflow** (a synchronous call that is one step of the caller's
    run): make the extracted context the parent, so one run is one trace.
  - **Its own workflow** (typically a queue, event or job consumer agreed as a
    separate workflow): start a new root span named after the workflow, with
    `gen_ai.agent.name`, and attach the extracted context as a span link, not
    as the parent. Otherwise its runs are filed under the sender's workflow.
    Replace any workflow name carried in baggage with its own, so the span
    processor does not stamp the sender's name on its spans. Tracium does not
    show links yet, but other tracing backends on the same provider do, so the
    two traces stay connected there.
- Every service on a parent path should export to Tracium, unless the user
  chose GenAI-only export (step 4). Otherwise traces arrive without their root
  span and lose their workflow name.

## Step 7: Verify

Verify on the stack the user actually runs, not only with unit tests or
synthetic scripts.

- Confirm the running code is the instrumented code: the dev server's working
  directory and branch, the deployed or emulated functions' code and
  environment variables, and that containers reach the Tracium endpoint.
  A stack started from another checkout sends nothing, with no error.
- For each repository, note the time, then run the app, a test, or a small
  script that makes one real LLM call through an instrumented path. If the
  available credentials cannot make a real call, say so and ask the user to
  run one.
- Wait a few seconds, then read the setup checks for spans since that time with
  the ingest key:

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

- For a run that spans services, run one end to end and confirm with the user
  (or in the local Tracium database, if you started it) that all its spans
  share one trace and the agreed workflow name. Several traces for one run
  mean a hop is missing propagation (step 6).

Fix what the checks and the user report, then check again with a new `SINCE`.
The checks name the problem and the fix: no LLM spans,
unpriced model, missing model, unmetered streams, instant LLM spans, errors
without detail, missing root span, unnamed workflow, workflow without version,
version per run, and spans rejected at ingest. Some are expected after an
agreed choice (`missing_root_span` with GenAI-only export,
`unnamed_workflow` for services deliberately left without workflow names);
tell the user which ones and why.

## Step 8: Report

End with a report to the user, one section per repository:

- **Path taken**: A, B, C or D, and why, and whether an LLM-only filter was
  added.
- **Boundaries**: each link to another repository, and whether it continues
  the sender's trace or starts its own with a span link.
- **Existing instrumentation**: what you found and confirm it is unchanged.
- **Files changed** and **dependencies added**.
- **Tracium attributes**: each one set, its value or source, and where it is
  set.
- **Workflows and trace scope**: what one run is, how its name reaches every
  span, and which spans are exported.
- **Environment variables**: names added, where they need values (local,
  local emulators, CI, each deploy target).
- **Verification**: what you ran, on which stack, and what showed up in
  Tracium.
- **Open items**: anything the user must still do, such as deploy secrets,
  price entries, services that do not export yet, hops without propagation,
  non-LLM services shown at $0, and setup checks still showing.

Do not commit or push unless the user asks.
