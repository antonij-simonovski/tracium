# api

The read half of Tracium. It serves trace and metric data to the dashboard over
a versioned REST API (`/v1/…`). It reads span data from ClickHouse and
config/accounts from Postgres. It never ingests spans and never writes to the
span store; that's [`collector`](../collector).

## The one rule that shapes everything

**Every read is bounded to a time window.** The `spans` table is ordered by
`start_time_ms`, so a windowed query prunes to its granules instead of scanning
the table; an unbounded read would get slower for every user as the table grows.
Concretely:

- Trace listings without an explicit lower bound default to the last 30 days
  (`internal/query/filters.go`), and page size is capped at 200.
- Metrics windows of 24h/7d/30d read raw spans; 90d/1y are served from the daily
  rollup (`tracium.metrics_daily`) so their cost tracks cardinality, not span
  volume.

If you add a query, it must fit this shape. There is no code path that scans the
span table unbounded, and there must never be one.

## Design

Handlers depend on the `TraceRepository` / `MetricsRepository` **interfaces**, not
on ClickHouse. The concrete store is wired once in `cmd/api/main.go`; swapping it
(or the no-op stub used in tests) is a one-line change. Errors are typed
(`ErrNotFound` maps to 404, everything else to 500), so handlers never string-match
on error text.

```
cmd/api/main.go        standalone entrypoint
app/                   reusable assembly, extension routes, lifecycle
extension/             mail and entitlement contracts
migrations/            embedded, tracked Postgres schema
internal/handler/      HTTP handlers (traces, spans, metrics, auth, workspaces)
internal/query/        ClickHouse repository + the windowing/rollup logic
internal/auth/         accounts, password hashing, JWT issuing (Postgres-backed)
internal/middleware/   CORS, API version prefix, auth, rate limiting
internal/model/        response types (mirrors spec)
testing/mocks/         interface mocks, the only backend unit tests touch
```

## Run

```bash
go run ./cmd/api      # needs CLICKHOUSE_DSN, POSTGRES_DSN, JWT_SECRET
go test ./...
```

Config comes from env vars and is validated at startup: a missing DSN or
`JWT_SECRET` exits the process with a listed error rather than starting in a
degraded state. Copy [`.env.example`](.env.example) to `.env` for local dev.
`make up` from the repo root publishes ClickHouse (8123) and Postgres (5432) on
localhost for this.

| Env | Required | Notes |
|---|---|---|
| `CLICKHOUSE_DSN` | yes | span data (HTTP scheme selects the HTTP protocol) |
| `POSTGRES_DSN` | yes | accounts/config; auth cannot run without it |
| `JWT_SECRET` | yes | signs **and** verifies admin tokens, never a shared default |
| `LISTEN_ADDR` | no | defaults to `:8090` |

**Auth fails closed.** There is no way to run the API without authentication.

## Routes

All prefixed `/v1`. Health and auth are unauthenticated; everything else requires
a bearer token. Changing a password ends every session issued before it.

```
GET  /health  /ready
POST /auth/register  /auth/login
GET|POST /workspaces        DELETE /workspaces/{id}         (auth only)
POST /workspaces/{id}/members   DELETE /workspaces/{id}/members/{userId}   (owner only)
GET  /traces  /traces/{id}  /traces/{traceId}/spans        (auth)
GET  /metrics/kpis  /cost-series  /latency-series  /error-series
GET  /metrics/top-workflows  /workflows  /workflows/{name}  /failures
GET  /metrics/workflows/{name}/versions
GET  /metrics/model-costs  /usage-users  /usage-workflows
GET  /metrics/attribute-keys  /usage-by-attribute        (allocate cost by any attribute)
```

The canonical request/response shapes live in
[`spec/api/openapi.yaml`](../spec/api/openapi.yaml), not here.

## Workspace access

Telemetry is scoped by **workspace**, which is the access boundary. A workspace
has **members** (`workspace_members`): its creator is the `owner`, and the owner
can add other accounts as `member` via the member endpoints above. Every span
carries a `workspace_id` (promoted from `tracium.workspace.id`); every read is
scoped to the workspaces the caller is a member of:

- An optional `workspace_id` query param (on `/traces` and every `/metrics/*`)
  narrows a read to one workspace; the dashboard passes the active one from its
  workspace switcher.
- If the caller is **not** a member of the requested workspace, the read is
  refused with **403**.
- With no `workspace_id`, a read returns the union of the caller's workspaces; an
  account that is a member of none sees nothing (never everything).

`user_id` is a *different* axis: the end-client/metering label the dashboard
allocates cost by, an optional filter, not an access boundary.

Single-trace reads and their `/spans` endpoint enforce the same membership
scope in the database. Without access they return 404; explicitly requesting an
inaccessible workspace returns 403. A trace ID is never an access credential.

## License

Apache 2.0. See [LICENSE](LICENSE).

## Application composition

The standalone command uses the public `app` package. Additional applications can
register authenticated routes, email delivery, entitlement policies, and namespaced
migrations without copying this API. See [the extension guide](../docs/extending.md).
