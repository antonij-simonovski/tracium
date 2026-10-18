# Securing the collector's OTLP ports

**Ingest requires a per-workspace API key on every request.** Both OTLP ports
(`4317` gRPC, `4318` HTTP) are guarded by the `traciumauth` authenticator: a
request with no key, or an unknown or revoked one, is rejected with `401` and
nothing is stored. There is no anonymous or shared-token path; the key is the
only way in, and it is not configurable off.

## How it works

Each sender uses its own Tracium API key (a `trc_…` token). On every request the
collector's `traciumauth` extension forwards the presented key to the API's
verify endpoint, which owns the key store, and caches the answer briefly. The
extension holds no keys itself and touches no database, so the collector stays a
generic OTel distribution while the API remains the single source of key truth.

A key is bound to **one workspace**: verifying it both authenticates the sender
and decides which workspace the telemetry lands in.

## Issue a key

Create one key per sender, either from the dashboard's **API keys** screen or via
the API:

```
POST /v1/workspaces/{id}/api-keys      (authenticated; you must be a member of the workspace)
```

The plaintext token (`trc_…`) is returned **once**, so capture it then; only its
hash is stored. Senders set only the key, no workspace attribute:

```bash
OTEL_EXPORTER_OTLP_HEADERS="Authorization=Bearer <api-key>"
```

Revoke a key with `DELETE /v1/workspaces/{id}/api-keys/{keyId}`; ingest stops
accepting it within the extension's `cache_ttl` (default 60s).

Expired authorization is rejected during verification outages by default. An
operator may explicitly set `max_stale_age` to allow an outage grace period;
it is an absolute age since the last successful verification, so revocation
can then take up to the larger of `cache_ttl` and `max_stale_age`. Repeated
outage responses never extend that deadline. Canceled requests never refresh
cached authorization, even with this option enabled.

## Configuration

Auth is on by default in the shipped [`collector/config/collector.yaml`](../../collector/config/collector.yaml):
`traciumauth` is listed under `service.extensions` and referenced by both
receiver protocols, and the `tracium` processor drops any span that somehow
reaches it unauthenticated. The one setting a deployment must supply is where the
API lives:

- **`INGEST_VERIFY_URL`**: the API's verify endpoint, e.g.
  `http://api:8090/v1/ingest/keys/verify`. Compose and the Helm chart set this to
  the in-cluster API service automatically.

`cache_ttl` bounds how long a revoked key keeps working (keep it short);
`cache_max_entries` caps how many verifications are held in memory (an LRU bound,
a security control because the cache is keyed by a sender-supplied token).

`sender_verify_limit` / `sender_verify_window` cap how many verify calls one
sender (peer address) can force against the API per window. Only cache misses
count, so a sender presenting one valid key is charged once and then served from
cache, so legitimate high-volume ingest is untouched. This stops a single abusive
sender, streaming distinct invalid keys, from spending the collector's shared
verify budget and blocking verification for everyone else behind the same
collector. Set `sender_verify_limit` to `0` to disable it. Behind NAT or an L4
load balancer every sender shares one peer address, so raise the limit or set
`max_stale_age`, which lets a limited sender keep using recently verified keys.

### The key decides the workspace

On verification the API returns the key's single workspace, and the collector
**stamps it onto every span** the request carries, overriding any
`tracium.workspace.id` the sender set. So:

- The client does not send a workspace attribute; the key is the source of truth.
- A sender cannot land data in a workspace other than its key's. It can't claim
  a workspace, the key grants exactly one.
- To send to several workspaces, use one key per workspace.

## Network isolation is still worth it

Even though ingest is authenticated, keeping the OTLP ports off the public
internet is sound defense in depth. The Helm chart already does this: the
collector is a ClusterIP service and the ingress only routes the API (`/v1`) and
dashboard (`/`), never `4317/4318`. In Docker Compose the ports are published to
the host on loopback (`127.0.0.1:4317`, `127.0.0.1:4318`) for local convenience.

## Rate limiting

Auth is not rate limiting. A valid but noisy sender can still flood ingest. Cap
request rate at the ingress or gateway (nginx `limit_req`, Envoy rate limits).
