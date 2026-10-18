#!/usr/bin/env bash
# Bounded operational checks on disposable containers. Requires Docker + Python 3.
set -euo pipefail
umask 077
cd "$(dirname "$0")/../../.."
check_dir=$(mktemp -d /tmp/tracium-production.XXXXXX)
check_project="tracium-production-$$"
report_dir="${TRACIUM_REPORT_DIR:-$PWD/deploy/reports/production-$(date +%Y%m%d-%H%M%S)}"
mkdir -p "$report_dir"
compose=(docker compose --project-name "$check_project" --env-file "$check_dir/env" -f docker-compose.yml -f compose.build.yaml -f deploy/tests/production/compose.yaml)
cleanup() {
  code=$?
  "${compose[@]}" logs --no-color --tail=150 > "$report_dir/compose.log" 2>&1 || true
  "${compose[@]}" down --volumes --remove-orphans >/dev/null || true
  echo "Reports: $report_dir"
  exit "$code"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
printf 'JWT_SECRET=%s\nCLICKHOUSE_PASSWORD=%s\nPOSTGRES_PASSWORD=%s\n' \
  "$(openssl rand -hex 32)" "$(openssl rand -hex 24)" "$(openssl rand -hex 24)" > "$check_dir/env"
if [ -n "${CHECK_IMAGE_PROJECT:-}" ]; then
  for component in api collector dashboard migrate; do
    docker image inspect "$CHECK_IMAGE_PROJECT-$component:latest" >/dev/null
  done
  python3 - "$CHECK_IMAGE_PROJECT" "$check_dir/images.yaml" <<'PY2'
import sys
from pathlib import Path
Path(sys.argv[2]).write_text('services:\n' + ''.join(f'  {x}:\n    image: {sys.argv[1]}-{x}:latest\n' for x in ('api','collector','dashboard','migrate')))
PY2
  compose+=(-f "$check_dir/images.yaml")
  "${compose[@]}" up -d --no-build
else
  "${compose[@]}" up -d --build
fi
export CHECK_PROJECT="$check_project" CHECK_DIR="$check_dir" CHECK_REPORT_DIR="$report_dir"
export API_URL="http://$("${compose[@]}" port dashboard 3000)"
export OTLP_URL="http://$("${compose[@]}" port collector 4318)"
export COLLECTOR_METRICS_URL="http://$("${compose[@]}" port collector 8888)"
python3 deploy/tests/production/checks.py

python3 deploy/tests/production/disk.py
bash deploy/tests/production/backup.sh "${compose[@]}"

# Test failures are reported in JSON and also reflected in the command exit code.
python3 - <<'PY2'
import json,os,sys
from pathlib import Path
p=Path(os.environ['CHECK_REPORT_DIR'])
checks=json.loads((p/'operational.json').read_text())
checks += [json.loads((p/f).read_text()) for f in ('disk.json','backup.json')]
summary={'passed':all(x.get('passed') is True for x in checks),
         'checks':[{'check':x['check'],'passed':x.get('passed')} for x in checks]}
(p/'summary.json').write_text(json.dumps(summary,indent=2))
sys.exit(0 if summary['passed'] else 1)
PY2
