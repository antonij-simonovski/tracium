#!/usr/bin/env bash
# Disposable kind cluster; never reads or writes the user's normal kubeconfig.
set -euo pipefail
cd "$(dirname "$0")/../../.."
report="${TRACIUM_REPORT_DIR:-$PWD/deploy/reports/kubernetes-$(date +%Y%m%d-%H%M%S)}"
mkdir -p "$report"
umask 077
cluster="tracium-validation-$$"
dir=$(mktemp -d /tmp/tracium-kind.XXXXXX)
kind="${KIND_BIN:-kind}"
helm="${HELM_BIN:-helm}"
source_project="${CHECK_IMAGE_PROJECT:?set the source project for validation images}"
kube=(kubectl --kubeconfig "$dir/kubeconfig" --context "kind-$cluster")
cleanup() {
  code=$?
  "${kube[@]}" get pods,pvc -A -o wide > "$report/kubernetes-final-state.txt" 2>&1 || true
  if [ "${cluster_created:-false}" = true ]; then "$kind" delete cluster --name "$cluster" >/dev/null 2>&1 || true; fi
  rm -f "$dir/kubeconfig"
  exit "$code"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# kind duplicates image layers inside its node; leave headroom for existing work.
python3 - "$report" <<'PY2'
import json,shutil,subprocess,sys
from pathlib import Path
memory=int(subprocess.check_output(['docker','info','--format','{{.MemTotal}}']))
free=shutil.disk_usage('.').free
if memory < 6*1024**3 or free < 8*1024**3:
    result={'check':'helm_install','passed':None,'status':'blocked_environment',
            'docker_memory_bytes':memory,'host_free_disk_bytes':free,
            'note':'This harness requires at least 6 GiB Docker memory and 8 GiB host free disk. No cluster was created.'}
    (Path(sys.argv[1])/'kubernetes.json').write_text(json.dumps(result,indent=2))
    print(result['note'],file=sys.stderr)
    sys.exit(2)
PY2
command -v "$kind" >/dev/null
command -v "$helm" >/dev/null
# Run only after the Compose workload is gone; the Docker VM has 4 GB RAM.
for attempt in $(seq 1 180); do
  if [ -z "$(docker ps -q --filter "label=com.docker.compose.project=$source_project")" ]; then break; fi
  sleep 5
done
if [ -n "$(docker ps -q --filter "label=com.docker.compose.project=$source_project")" ]; then
  echo 'Compose tests are still running; refusing to overlap resource-heavy checks.' >&2; exit 1
fi
cluster_created=true
"$kind" create cluster --name "$cluster" --kubeconfig "$dir/kubeconfig" --image kindest/node:v1.34.0 --wait 120s
# Keep the test cluster within the remaining Docker memory budget.
docker update --memory 2300m --memory-swap 2300m --cpus 3 "$cluster-control-plane" >/dev/null
for component in api collector dashboard migrate; do
  "$kind" load docker-image "$source_project-$component:latest" --name "$cluster"
done
"$kind" load docker-image clickhouse/clickhouse-server:24.6 postgres:16-alpine --name "$cluster"
"${kube[@]}" create namespace tracium-validation
kube+=(--namespace tracium-validation)
ch_pass=$(openssl rand -hex 24);pg_pass=$(openssl rand -hex 24);jwt_secret=$(openssl rand -hex 32)
"${kube[@]}" create secret generic tracium-clickhouse-secret --from-literal="password=$ch_pass"
"${kube[@]}" create secret generic tracium-postgres-secret --from-literal="password=$pg_pass"
"${kube[@]}" create secret generic tracium-api-jwt --from-literal="jwt-secret=$jwt_secret"
cat > "$dir/values.yaml" <<YAML
global:
  imageTag: latest
  imagePullPolicy: Never
collector:
  replicaCount: 1
  image: {repository: $source_project-collector}
  resources: {requests: {cpu: 100m, memory: 128Mi}, limits: {cpu: '1', memory: 256Mi}}
  queue: {storageSize: 128Mi}
api:
  replicaCount: 1
  image: {repository: $source_project-api}
  resources: {requests: {cpu: 50m, memory: 64Mi}, limits: {cpu: '1', memory: 128Mi}}
dashboard:
  image: {repository: $source_project-dashboard}
  resources: {requests: {cpu: 25m, memory: 32Mi}, limits: {cpu: 500m, memory: 64Mi}}
migrate:
  image: {repository: $source_project-migrate}
clickhouse:
  storageSize: 256Mi
  resources: {requests: {cpu: 100m, memory: 256Mi}, limits: {cpu: '1', memory: 768Mi}}
postgres:
  storageSize: 128Mi
  resources: {requests: {cpu: 50m, memory: 64Mi}, limits: {cpu: 500m, memory: 128Mi}}
YAML
helm_args=(--kubeconfig "$dir/kubeconfig" --kube-context "kind-$cluster" --namespace tracium-validation)
set +e
"$helm" install tracium deploy/helm/tracium "${helm_args[@]}" -f "$dir/values.yaml" --timeout 300s > "$report/helm-install.txt" 2>&1
install_code=$?
if [ "$install_code" = 0 ]; then
  for workload in statefulset/tracium-clickhouse statefulset/tracium-postgres deployment/tracium-api statefulset/tracium-collector deployment/tracium-dashboard; do
    "${kube[@]}" rollout status "$workload" --timeout=180s || install_code=$?
  done > "$report/kubernetes-rollout.txt" 2>&1
fi
set -e
"${kube[@]}" get pods,pvc -o wide > "$report/kubernetes-install-state.txt"
"${kube[@]}" get events --sort-by=.lastTimestamp > "$report/kubernetes-install-events.txt"
"${kube[@]}" logs job/tracium-migrate > "$report/kubernetes-migration.txt" 2>&1 || true
printf '{"check":"helm_install","passed":%s,"exit_code":%s,"note":"One-node kind 1.34.0; local images, one replica and reduced resource/storage requests; documented secrets only"}\n' \
  "$([ "$install_code" = 0 ] && echo true || echo false)" "$install_code" > "$report/kubernetes.json"

# Store a probe row, replace the DB pod, and verify the PVC still carries it.
set +e
"${kube[@]}" exec tracium-clickhouse-0 -- sh -c 'clickhouse-client --password "$CLICKHOUSE_PASSWORD" --query "INSERT INTO tracium.spans (trace_id,span_id) VALUES ('"'kube-persistence','span'"')"' > "$report/kubernetes-persistence.txt" 2>&1
insert_code=$?
"${kube[@]}" delete pod tracium-clickhouse-0 --wait=true >> "$report/kubernetes-persistence.txt" 2>&1
"${kube[@]}" rollout status statefulset/tracium-clickhouse --timeout=120s >> "$report/kubernetes-persistence.txt" 2>&1
"${kube[@]}" exec tracium-clickhouse-0 -- sh -c 'clickhouse-client --password "$CLICKHOUSE_PASSWORD" --query "SELECT count() FROM tracium.spans WHERE trace_id='"'kube-persistence'"'"' >> "$report/kubernetes-persistence.txt" 2>&1
persistence_code=$?
set -e
printf 'insert_exit=%s read_exit=%s\n' "$insert_code" "$persistence_code" >> "$report/kubernetes-persistence.txt"
echo "Kubernetes evidence written to $report"
