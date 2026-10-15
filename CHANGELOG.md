# Changelog

All user-visible changes (schema, API, collector, dashboard, deployment) are recorded here. Follow the format below when adding entries.
Breaking changes require a `schema_version` bump. Additive changes do not.

---

## [1.1.0] - Unreleased

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
