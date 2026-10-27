# deploy/

Infrastructure for running Tracium: the Helm chart, the schema-migration runner,
and runbooks. **The Docker Compose quickstart lives at the repo
root** (`docker compose up`, see the top-level [README](../README.md));
this directory is the Kubernetes/production side plus the shared pieces both use.

```
deploy/
  helm/tracium/     Helm chart (StatefulSets, PVCs, ConfigMaps, Ingress)
  docker/migrate/   migration-runner image (also used by the root compose)
  docs/             collector-auth, rollup-repair runbooks
```

## Kubernetes (Helm)

Each release attaches a packaged chart (`tracium-<version>.tgz`) that requests
that release's images from `ghcr.io/antonij-simonovski`. Create the secrets the
chart references, then install it:

```bash
kubectl create secret generic tracium-clickhouse-secret --from-literal=password=$(openssl rand -hex 16)
kubectl create secret generic tracium-postgres-secret   --from-literal=password=$(openssl rand -hex 16)
kubectl create secret generic tracium-api-jwt           --from-literal=jwt-secret=$(openssl rand -hex 32)
VERSION=1.2.3
helm install tracium "https://github.com/antonij-simonovski/tracium/releases/download/v$VERSION/tracium-$VERSION.tgz"
```

Don't pass `--wait`: schema migrations run as a post-install hook, and the API
and collector only become ready once they finish. The API and collector restart a
few times while the migration job runs; that is expected.

To install from a checkout instead, run `make sync-generated` and pass
`--set global.imageTag=<version>`; the source chart's `appVersion` is not a
release.

Every tunable is documented in [`helm/tracium/values.yaml`](helm/tracium/values.yaml).
Ingress exposes the API and dashboard. OTLP and database services stay internal.

Browser login limits use the original client IP. Compose trusts only the
`dashboard` service's resolved addresses; Helm uses a dedicated headless
dashboard Service to discover proxy pod addresses. The API refreshes these
addresses every five seconds on demand and ignores forwarded headers from
other peers. Keep service discovery under deployment control.

When enabling Helm ingress, also set `ingress.trustedProxies` to the ingress
controller's actual peer IPs/CIDRs, or `dns:` followed by a headless Service
name that resolves to its pods. The chart refuses an ingress configuration
without this list. The controller must sanitize `X-Forwarded-For`; do not
trust the entire pod network. Standalone API deployments can use the same
IP/CIDR/`dns:service-name` entries in `AUTH_TRUSTED_PROXIES`.

To let MCP clients such as Claude connect by signing in, set `api.publicURL` to
the Ingress origin (e.g. `--set api.publicURL=https://tracium.example.com`). The
Ingress already sends `/.well-known/oauth-protected-resource` and
`/.well-known/oauth-authorization-server` to the API; a reverse proxy of your own
must do the same, and serve `/oauth/authorize` from the dashboard.

Existing installations: follow [the workspace upgrade guide](docs/upgrading.md).

## Notes

- **Pricing & schema** are the source-of-truth files in `spec/` and
  `collector/schema/`. The Helm chart needs its own copies under
  `helm/tracium/files/`; regenerate them with **`make sync-generated`** from the
  repo root after any change (don't hand-edit the copies).
- **OTLP ingest requires a per-workspace API key**: [securing the collector](docs/collector-auth.md).
- **Runbook:** [repairing the daily rollup](docs/rollup-repair.md).
