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
GET|POST /account/api-keys  DELETE /account/api-keys/{keyId}   (your read keys; auth only)
POST /workspaces/{id}/members   DELETE /workspaces/{id}/members/{userId}   (owner only)
GET  /traces  /traces/{id}  /traces/{traceId}/spans        (auth)
GET  /metrics/kpis  /cost-series  /latency-series  /error-series
GET  /metrics/top-workflows  /workflows  /workflows/{name}  /failures
GET  /metrics/workflows/{name}/versions
GET  /metrics/model-costs  /usage-users  /usage-workflows
GET  /metrics/attribute-keys  /usage-by-attribute        (allocate cost by any attribute)
POST /query   GET /query/schema   POST /mcp                (auth or read key)
POST /mcp/{key}                                            (read key in the URL, for connectors)
POST /oauth/register  /oauth/token                         (OAuth for MCP clients; needs PUBLIC_URL)
GET|POST /oauth/authorize                                  (consent page's calls; auth only)
```

`/query` takes a structured query (metrics, dimensions, filters, granularity,
formulas over the metrics, `having` on them), or `columns` to list matching
calls or runs, and compiles it in `internal/query/custom.go`; no SQL is
accepted. Fields are the curated dimensions plus every column ClickHouse
reports for `tracium.calls`, so a column a migration adds is queryable without
code changes. On calls, `run.<field>` and `parent.<column>` join each call's run
and parent span; on runs, per-run totals such as `cost_usd` are fields. Filters
are AND-ed, with `any` for alternatives. Results page with
`page` and `meta.next_page`. `/mcp` is a stateless MCP server over the same queries. Both also
accept a read key, created under Settings → Account. A read key acts as your
account, so an MCP client can reach every workspace you are a member of and
picks one per call with `workspace_id` (see the `list_workspaces` tool).

Creating a read key shows a connector URL, `https://your-tracium/v1/mcp/trc_...`,
which is all a client needs: paste it as the server URL of a custom connector in
Claude, or run:

```bash
claude mcp add --transport http tracium https://your-tracium/v1/mcp/trc_...
```

The URL is a secret. The bundled nginx does not log `/v1/mcp/` paths, but the
Helm chart's Ingress sends `/v1` straight to the API, and most ingress
controllers log full paths by default. Exclude `/v1/mcp/` from those access logs,
or use clients that can send headers: `POST /v1/mcp` with
`Authorization: Bearer trc_...`.

### Connecting with OAuth

Set `PUBLIC_URL` (`server.public_url`, Helm `api.publicURL`) to the origin you
open Tracium at, such as `https://tracium.example.com`. MCP clients that
support OAuth, Claude among them, then need only `https://your-tracium/v1/mcp`:
the client registers itself, opens Tracium's `/oauth/authorize` page, and once
you sign in and click Allow it receives a new read key named after the client.
Nothing is registered with an outside provider, and the key never appears in a
URL. Revoke it under Settings → Account to disconnect the client.

```bash
claude mcp add --transport http tracium https://your-tracium/v1/mcp
```

A 401 from `POST /v1/mcp` points the client at
`/.well-known/oauth-protected-resource/v1/mcp`, which names Tracium as its
authorization server (`/.well-known/oauth-authorization-server`). The bundled
nginx and the Helm Ingress route both to the API. Only the authorization code
flow with S256 PKCE is supported, codes last a minute and work once, and
redirect URIs must be https, loopback http, or an app's own scheme. Claude's
hosted connectors call Tracium from Anthropic's servers, so they need a
`PUBLIC_URL` reachable from the internet; Claude Code and other local clients
work with an internal one.

`/query` and both `/mcp` routes share one rate limit of 120 requests per minute
per account, since hosted connectors share egress IPs (`AUTH_QUERY_RATE_LIMIT_PER_MINUTE`; negative disables).

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
