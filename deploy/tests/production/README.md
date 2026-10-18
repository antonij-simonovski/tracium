# Production validation

These destructive fault tests run only against a new, disposable Compose project
with random loopback ports, generated test credentials, and dedicated volumes.
They do not use the root `.env`. They require Docker Compose with `!override`
support, Python 3, OpenSSL, and enough headroom for about 2 GiB of test containers.

From the repository root:

```sh
bash deploy/tests/production/run.sh
```

The runner builds the current source, runs the checks, restores backups into
separate empty volumes, and removes its test containers and volumes on exit.
JSON results and logs are saved under `deploy/reports/production-<timestamp>`.
Generated test credentials and backup archives remain in the private temporary
directory printed in Compose diagnostics. Treat raw reports and backups as local
artifacts; review them before sharing. A nonzero exit means a failed or incomplete
check; use the JSON results to distinguish application failures from resource limits.

`CHECK_NAMES=auth,crash,scale` selects a subset of the Python checks. Other names
are `load` and `outage`. Disk exhaustion and backup restoration still run.
`TRACIUM_REPORT_DIR` chooses an absolute report directory.
`CHECK_IMAGE_PROJECT` optionally reuses images built by a previous validation
project; omit it when source changes so the runner builds current images.

The load check targets 500 spans/second for two minutes. The outage check keeps
ClickHouse down for six minutes, kills the collector after its batch flush, and
checks recovery. The immediate-crash check kills the collector after an OTLP
acknowledgement but before the five-second batch timeout. Disk exhaustion fills
only a private 16 MiB tmpfs. The scale check attempts 100,000 and 1,000,000
synthetic spans; its 1 GiB ClickHouse limit may stop the larger insertion.

These are bounded diagnostics, not certification of throughput, high availability,
or an exhaustive security review. A successful HTTP response is checked against
stored rows; HTTP latency is not a measurement of durable ingestion latency.

## Kubernetes

On a machine with at least 6 GiB Docker memory and 8 GiB free host disk, install
kind, Helm, and kubectl, then run:

```sh
CHECK_IMAGE_PROJECT=tracium-production-<build-project-id> \
  bash deploy/tests/production/kubernetes.sh
```

`KIND_BIN` and `HELM_BIN` can select explicit binaries. The script uses a separate
kubeconfig and a new kind cluster, reduced resource requests, and locally built
images. It installs the unchanged chart with the documented secrets, waits for
every workload to roll out, and checks that ClickHouse data survives a pod
replacement. The cluster is deleted on exit. Existing user clusters and
kubeconfig are untouched.
