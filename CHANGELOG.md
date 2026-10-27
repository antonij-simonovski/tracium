# Changelog

All user-visible changes (schema, API, collector, dashboard, deployment) are recorded here. Follow the format below when adding entries.
Breaking changes require a `schema_version` bump. Additive changes do not.

---

## [Unreleased]

### Added
- MCP clients can connect with OAuth instead of a pasted read key. Set `PUBLIC_URL` (`server.public_url`, Helm `api.publicURL`) to the origin you open Tracium at, and give a client such as Claude just `https://your-tracium/v1/mcp`: it registers itself, the user signs in and clicks Allow on the dashboard's new `/oauth/authorize` page, and the client receives a new read key named after it, revoked under Settings → Account. Tracium is its own authorization server, so nothing is registered with an outside provider. New routes: `/.well-known/oauth-protected-resource[/v1/mcp]`, `/.well-known/oauth-authorization-server`, `POST /v1/oauth/register` (RFC 7591), `GET|POST /v1/oauth/authorize` and `POST /v1/oauth/token`. A 401 from `POST /v1/mcp` carries `WWW-Authenticate` with the resource metadata URL. Only the authorization code flow with S256 PKCE for public clients is supported; codes last a minute and work once. Register and token share the login rate limit. Postgres migration `006` adds `oauth_clients` and `oauth_codes`. Without `PUBLIC_URL` nothing changes
- The bundled nginx and the Helm Ingress route `/.well-known/oauth-*` to the API, and nginx serves `/oauth/authorize` with `frame-ancestors 'none'`

---

## [1.2.1] - 2026-10-21

### Fixed
- The dashboard library ships its stylesheet as `dist-library/style.css` again, so `tracium-dashboard/style.css` resolves. Vite 8 had renamed it to `tracium-dashboard.css`

---

## [1.2.0] - 2026-10-21

### Added
- MCP server at `POST /v1/mcp` (stateless Streamable HTTP) with `list_workspaces`, `describe_schema`, `query`, `list_traces`, `get_trace` and `detect_anomalies` tools, so an AI client can answer questions about your own telemetry. Each tool takes an optional `workspace_id`; `list_traces` and `get_trace` take `fields` (and `span_fields`) to return only what the client needs, and page with `page` (`span_page`). Tools are annotated read-only, results are returned once as text, and requests whose `Origin` is not the server's own host are refused. Creating a read key shows one connector URL, `/v1/mcp/<key>`, to paste into Claude's custom connector dialog or any MCP client; the bundled nginx keeps that path out of its access log, but a Helm Ingress logs it unless configured not to. `/v1/query` and both MCP routes share a rate limit of 120 requests per minute per account (`AUTH_QUERY_RATE_LIMIT_PER_MINUTE`), since hosted connectors such as Claude's share egress IPs
- `POST /v1/query` and `GET /v1/query/schema`: structured custom queries over calls or runs, with metrics, up to 4 dimensions (including `attr.<key>` custom attributes), filters and hourly or daily buckets, over windows up to 92 days. On calls, every stored column is a field, read live from ClickHouse so new columns need no code: group, filter (`gt`/`lt`/`is_empty` and more), aggregate as `<func>:<field>` (e.g. `max:cost_usd`), or list matching calls with `columns`. On calls, `run.<field>` reads the call's run (`run.errored`, `run.cost_usd`, `run.attr.<key>`) and `parent.<column>` its parent span. On runs, per-run totals (`cost_usd`, `duration_ms`, `errored`, `call_count`, tokens) and `attr.<key>` are fields, so runs can be filtered, listed with `columns` and aggregated with `<func>:<field>`. Filters take `any` for OR, `formulas` compute from the metrics (e.g. `cost / runs`), and `having` filters on metrics and formulas. Results page with `page`. No SQL is accepted; names are checked against the catalog and live columns, and every query is limited to the caller's workspaces. Cost matches the dashboard (`meta.cost_source: reconciled`) when grouped only by user or workspace
- Read keys, under Settings → Account and `/v1/account/api-keys`. A read key belongs to the account that created it and acts as that account on the query API, MCP and setup checks, reading the workspaces the account is a member of at request time. The collector rejects read keys for ingest, and ingest keys cannot use the query API or MCP. Postgres migration `005` adds `api_keys.scope` (`ingest` for every existing key) and lets `workspace_id` be null for read keys
- `tracium.cost_usd` set by the sender on a span with no model and no token counts (a paid tool or external API call) is stored as that span's cost and counted in run, workflow and client totals. Model calls, embeddings included, are still priced from their tokens and ignore it. A value below 0, not a number, or above $10,000 drops the span with the new code `cost_out_of_range`, which the setup checks report. Token metrics never meter this spend, so cost totals add it on top of the span and metric reconciliation. Migration `015` adds `reported_cost` to `tracium.metrics_daily_cost` and recreates its view; stop the collector while it runs
- `GET /v1/metrics/setup-checks` also accepts a workspace ingest key (`trc_...`) as the bearer token, scoped to the key's workspace and checked against the entitlements of the user who created the key, so a coding agent can verify instrumentation without a dashboard login. Every other route still requires a session token
- `since` filter on `GET /v1/metrics/setup-checks`, so a fix can be checked against new spans only
- `bootstrap` command in the API image (`docker compose exec api ./bootstrap --email you@example.com`) creates the account if missing, a workspace if it owns none (a workspace it was only invited to is never used), and an API key, and prints the key, so a local install needs no dashboard steps before ingest. It only runs with access to the API's database; there is no HTTP endpoint. `AGENT_SETUP.md` uses it for the local Docker path
- `AGENT_SETUP.md`, a playbook a coding agent follows to instrument an app with OpenTelemetry and send it to Tracium: it asks about workflows, trace scope and non-LLM services, sets the workflow name and version on every span, exports only the traces that call LLM services, reports non-LLM costs with `tracium.cost_usd`, and verifies the result with the setup checks. The README quickstart opens with the prompt that starts it

### Changed
- Spans with no model always pass the collector's `allowed_models` list, which now restricts model calls only. Before, it dropped every tool and API span as `unknown_model`
- A streamed model call with no token usage is flagged unmetered even if it carries `gen_ai.usage.cost` or `llm.usage.total_cost`; those attributes were never used for cost
- The collector is built on OpenTelemetry Collector v0.163.0 (OCB v0.163.0, `go.opentelemetry.io/otel` v1.69.0) with grpc 1.84 and clickhouse-go 2.49. A custom collector config must move the ClickHouse exporter's `batcher` block to `sending_queue.batch` (`min_size_items`/`max_size_items` become `min_size`/`max_size` with `sizer: items`) and `service.telemetry.metrics.address` to a `readers` pull exporter; the bundled config already does
- Images build with Go 1.27
- Dashboard: React 19, React Router 7, Vite 8 and Vitest 5. The dashboard library accepts React 18 or 19 as a peer

---

## [1.1.0] - 2026-10-16

### Added
- Workflow versions. A span's version is the resource's `tracium.workflow.version`; span attributes are not read. It is stored in the new `tracium.spans.workflow_version` column and added to the `metrics_daily` key (migration 014)
- `Trace.workflow_version`: the root span's version, else the earliest versioned span's
- `GET /v1/metrics/workflows/{name}/versions` lists a workflow's versions with runs, cost per run, p95 latency, error rate and tokens per run
- `version` filter on `GET /v1/traces` and the series endpoints
- Workflow detail, its series and the `version` filter serve 90d and 1y from the daily rollup, with latency unset, so the workflow and version compare pages work on long ranges
- `GET /v1/metrics/token-series` returns input and output tokens per time bucket; the Usage page charts them
- `span.json` lists `workflow_version`
- Setup checks `workflow_without_version` (info: runs with no `tracium.workflow.version`) and `version_per_run` (warning: a workflow in a workspace with at least 20 versioned runs where more than half carry a distinct version)
- The examples set `tracium.workflow.version`, and the demo seed ships two or three releases per workflow, with one unversioned workflow
- `Anomaly.version_change`: a workflow version first seen on the anomaly's day or the day before (for workspace-wide anomalies, of the workflow that carried the most of the metric, errored runs for error rate, and none when that workflow did not change version; a workflow's first versioned day does not count); the summary and the Overview outliers name it
- The smoke test checks versions end to end, and the upgrade test runs migration 014 on a 1.0.4 rollup
- Dashboard light mode, toggled from the sidebar; the default theme follows the system preference
- Collapsible dashboard sidebar
- Sidebar GitHub star button with the live star count

### Changed
- Migration 014 rebuilds `metrics_daily` with the version column in its sorting key and keeps the original as `metrics_daily_pre_version`. Stop ingestion while it runs; existing rollup rows read as unversioned.
- The repository moved to `antonij-simonovski/tracium`. Images are now published as `ghcr.io/antonij-simonovski/{collector,api,dashboard,migrate}`; `docker-compose.yml` and the Helm chart point there, and the old `ghcr.io/antonij-tracium` paths no longer resolve. Update any image overrides

---

## [1.0.4] - 2026-10-09

### Added
- Setup checks: `GET /v1/metrics/setup-checks` reports instrumentation problems (unpriced or missing models, streams without usage, LLM spans that end instantly, errors without a message, traces missing their root span, no LLM spans, workflows without `gen_ai.agent.name`, spans rejected at ingest) with how to fix each; the dashboard flags them in a banner on Overview
- `Span.setup_issues` lists the span-level checks a span fails
- `tracium.rejected_spans` (migration 012) counts rejected spans per workspace, hour and error code, written by the collector when `processors.tracium.dead_letter.clickhouse_dsn` is set
- `CAPTURE_CONTENT=false` (Helm: `collector.captureContent=false`) stops the collector storing prompt and completion text
- Helm values for pod and container security contexts, and `ingress.annotations`
- Dashboard extensions: `ingestEndpoint` sets the OTLP endpoint shown in the workspace connection instructions, which otherwise show the local collector

### Changed
- A span rejected for a missing ingest key is no longer attributed to the workspace its sender claimed
- `tracium.rejected_spans` rows expire after `RETENTION_DAYS`, like spans (migration 013)
- `POST /v1/auth/password` returns `200` with a new session token instead of `204`; changing a password, here or with the `reset-password` CLI, ends every earlier session
- `POST /v1/workspaces/{id}/members` always adds a `member`; the `role` field is gone and ownership can't be granted
- Auth and entitlement failures return the JSON `ErrorResponse`; a session that can't be verified because Postgres is unavailable gets `503 UNAVAILABLE` instead of `401`
- Login takes the same time whether or not the email has an account
- Metrics endpoints other than the series reject the `workflow` filter, and attribute usage rejects ranges longer than 30d
- The API shares one Postgres pool and runs its migrations once at startup; an invalid number in `AUTH_RATE_LIMIT_PER_MINUTE` or `AUTH_VERIFY_RATE_LIMIT_PER_MINUTE` fails startup
- All images run as a non-root user: uid 10001, or `nginx-unprivileged`'s uid 101 for the dashboard. `docker compose up` hands an existing collector queue volume to the collector's user
- The migrate image contains the schema migrations, so Compose no longer mounts `collector/schema`
- Images build with Go 1.26 and Node 24, matching CI
- Helm: ClickHouse probes use its HTTP `/ping`, an empty `storageClass` uses the cluster default, and the collector no longer receives unused env vars
- Dashboard: shared stat tiles, KPI strip and formatters; trend arrows are colored by whether the change is good or bad; large traces render faster; dialogs trap focus and close on Escape; pages recover from a crash on navigation
- Dashboard: the trace view shows the trace's client as a chip beside the start and end time

### Fixed
- `span.json` documents `workspace_id` as set from the verified ingest key and `model_normalized` as lower-cased only, with no alias resolution
- `span.json` lists `workflow_name`, `service_name`, `source` and `attributes`, and `available_tools` names `gen_ai.tool.definitions` as a source
- Workspace role casing, a `500` when revoking an API key, case-sensitive member email lookup, and readiness errors that leaked internal details
- The collector keeps the unmetered flag without content capture, matches `allowed_models` case-insensitively, treats permanent row errors as permanent, and prices Haiku 4.5
- Collector key verification no longer fails waiting requests when the coalescing request is cancelled, and serves stale keys to rate-limited senders
- The dashboard clears cached data on logout, reads daily chart dates in UTC, shows `$0.00` costs, and keeps data when a poll fails
- Workflow detail no longer shows another workflow's or workspace's numbers while loading, and its failure count matches the stats
- Usage and Clients: unattributed spend can't be opened as a client, a stale attribute key falls back to a valid one, and cost and runs pair by bucket time
- Signup asks for 8+ characters, matching the API
- Helm creates Postgres data in a subdirectory on new volumes and restarts pods when their config changes

### Removed
- `auth.mode=none` (`AUTH_MODE=none`); the API always requires authentication
- Unused API config: `server.health_addr`, `auth.token_header` and the `telemetry` section (`log_level`, `log_format` and the `LOG_LEVEL` / `LOG_FORMAT` env vars)

---

## [1.0.3] - 2026-10-04

### Added
- Node.js example that sends a GenAI-semconv trace without an LLM provider key (`examples/node`)
- README dashboard GIF and a "Works with" section listing supported instrumentation

### Changed
- `docker compose up` pulls the release images pinned by `TRACIUM_VERSION`; `compose.build.yaml` (used by `make up`) builds from source
- The collector health check is published on host port `13133` instead of `8080`
- ClickHouse and Postgres are no longer published on the host by default; `make up` publishes them through `compose.dev.yaml`
- Helm installs use the chart attached to each release; installing with `--wait` is not supported
- The dashboard package is marked `"type": "module"`
- Go workspace requires Go 1.26
- Dependencies: `golang.org/x/crypto` 0.57.0, `go.uber.org/zap` 1.28.0, `@tanstack/react-query` 5.104.0, `jsdom` 30.1.1, `@vitejs/plugin-react` 5.2.0
- Dashboard image builds on `node:26-alpine`; CI runs Node 24
- CI actions: `actions/checkout` v7, `actions/setup-go` v7, `docker/build-push-action` v7, `docker/login-action` v4, `docker/setup-qemu-action` v4

### Fixed
- The Helm chart requests images from `ghcr.io/antonij-tracium`, where releases publish them
- The workspace upgrade guide checks out the new release before migrating, and configures exporters with an ingest key
- Demo seed numbers in the READMEs match what the seed script sends

### Removed
- One-off review reports and harnesses under `deploy/reports` and `deploy/tests`
- Unused `deploy/config/`, `deploy/.env.example`, `deploy/scripts/wait-for-db.sh`, `spec/codegen/gen-go-models.sh` and pnpm files
- Enterprise and PII references from the documentation

---

## [1.0.2] - 2026-10-01

### Added
- Release images are published for `linux/arm64` as well as `linux/amd64`, so they pull natively on Apple Silicon and ARM nodes

### Changed
- Dashboard text is one step larger throughout, and secondary text uses a lighter gray for higher contrast

---

## [1.0.1] - 2026-10-01

### Added
- Workspace invites by email with single-use shareable links (`/v1/workspaces/{id}/invites`, `/v1/invites/{token}`)
- `extension.InviteNotifier` hook for delivering invite links, and `email_sent` on the create-invite response
- Per-workspace member limits via the `workspaces.members.add` entitlement (`403 MEMBER_LIMIT_REACHED`)
- `GET /v1/workspaces/{id}/members` and a Members tab in Settings
- `POST /v1/auth/password` to change your password, with a matching Settings form
- `reset-password` admin CLI for self-hosted deployments
- `extension.Sessions` for issuing sessions to accounts an embedding app authenticated itself
- Dashboard auth extension points: `authRoutes`, `forgotPasswordHref`, `signInOptions`, `signupFields`
- Dashboard exports `AuthDivider`, `EMAIL_KEY`, `storeSession` and `readPendingInvite`
- Client detail page with spend, runs, failure rate, latency, models, workflows and recent traces
- Trace view header shows and links to the trace's client
- OpenAPI docs for the password endpoints, the register/login confirmation responses, and register's `extensions` field

### Changed
- Dashboard Users pages renamed to Clients (`/clients`; old `/users` links still work)
- Time range picker shows on the Clients pages and no longer on the trace page
- Workspace setup panel walks through API-key ingest instead of showing the workspace ID
- `POST /v1/auth/password` returns `409 NO_PASSWORD` for accounts without a password
- Security reports now go to antonij@tracium.ai

### Fixed
- Corrected the 1.0.0 pricing note: reported costs are used only when `TrustReportedCost` is enabled

---

## [1.0.0] - 2026-09-20

### Changed
- Renamed `agent` to `workflow` across storage, API, and dashboard (breaking; migration `011`)
- Renamed the metering dimension `tenant` to `user` (`tracium.user.id`, `user_id`; breaking)
- `pricing.json` is generated from LiteLLM's pricing table, with cache-token and long-context pricing
- `LatencyBucket` percentiles are nullable, and latency series are null-filled

### Added
- Optional email confirmation through the `extension.AccountLifecycle` hook
- Caller-scoped entitlement gate for user-wide features
- Per-workspace ingest API keys; ingest now requires a key
- Workspaces as the access boundary (`workspace_id`, memberships, workspace endpoints)
- `output_tokens_derived` and `unmetered` metering provenance flags on `Span`
- Agent (now workflow) list and detail endpoints, and an agent filter on series and traces
- `90d` and `1y` ranges served from a daily rollup table
- `kind` field on `Span`
- `GET /v1/metrics/error-series`
- `agent_name` storage column for grouping traces per agent
- OpenAPI docs for the cost-allocation metrics endpoints

---

## [v4] - Metrics Ingestion (Unified Spans Source)

### Added
- The collector ingests OTLP metrics (`gen_ai.client.token.usage`) into `tracium.spans`
- `source` storage column (`span` or `metric`)
- Cost and token totals prefer metric rows when present

---

## [v1] - Baseline Schema

### Added
- Initial span, trace and error schemas
- Pricing table
- OpenAPI spec v0.1.0 with traces, health and ready endpoints
- OTel GenAI and Tracium attribute definitions
- Codegen scripts

---

## [v3] - Span Content & Tools

### Added
- `input`, `output` and `available_tools` fields on `Span`
- Content and tool source attribute docs

---

## [v2] - Error Fields

### Added
- `error_type` and `error_message` fields on `Span`

---

## Overview Metrics Endpoints

### Added
- `GET /v1/metrics/kpis`, `cost-series`, `latency-series`, `top-agents` and `failures`
- Shared `range` and optional `tenant_id` query parameters
