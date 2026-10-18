#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
smoke_dir=$(mktemp -d)
smoke_project="tracium-smoke-$$"
compose=(docker compose --project-name "$smoke_project" --env-file "$smoke_dir/env" -f docker-compose.yml -f compose.build.yaml -f deploy/tests/smoke.compose.yaml)
cleanup() {
  code=$?
  if [ "$code" -ne 0 ]; then "${compose[@]}" logs --tail=60; fi
  "${compose[@]}" down --volumes --remove-orphans >/dev/null
  rm -rf "$smoke_dir"
  exit "$code"
}
trap cleanup EXIT
printf 'JWT_SECRET=%s\nCLICKHOUSE_PASSWORD=%s\nPOSTGRES_PASSWORD=%s\n' \
  "$(openssl rand -hex 32)" "$(openssl rand -hex 24)" "$(openssl rand -hex 24)" > "$smoke_dir/env"
"${compose[@]}" up -d --build
export API_URL="http://$("${compose[@]}" port dashboard 3000)"
export OTLP_URL="http://$("${compose[@]}" port collector 4318)"
python3 deploy/tests/smoke.py
# A second migration run must preserve the ingested data and succeed.
"${compose[@]}" run --rm migrate

bash deploy/tests/upgrade.sh "${compose[@]}"
