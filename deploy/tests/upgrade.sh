#!/usr/bin/env bash
# Invoked only against the disposable stack created by smoke.sh.
set -euo pipefail
compose=("$@")
ch() {
  "${compose[@]}" exec -T clickhouse sh -c 'clickhouse-client --password "$CLICKHOUSE_PASSWORD" --multiquery' <<< "$1"
}
pg() { "${compose[@]}" exec -T postgres psql -U tracium -d tracium -v ON_ERROR_STOP=1 -tAc "$1"; }
assert_equal() { [ "$1" = "$2" ] || { echo "Assertion at line ${BASH_LINENO[0]}: expected $2, got $1" >&2; exit 1; }; }
"${compose[@]}" stop api collector
# Replace only this disposable test database with the previous release schema.
ch 'DROP DATABASE tracium; CREATE DATABASE tracium;'
for file in deploy/tests/fixtures/legacy/*.sql; do ch "$(cat "$file")"; done
pg "TRUNCATE schema_migrations; DROP TABLE IF EXISTS workspace_upgrade_state;"
# The previous release had no application migration ledger. The fresh-stack
# smoke test already applied the identity migration, so reset its ledger too.
# Otherwise the API skips the legacy owner backfill when it starts below.
pg "DROP TABLE app_schema_migrations;"
for name in 001_create_spans.sql 002_create_metrics_daily.sql 003_create_metrics_daily_mv.sql; do
  pg "INSERT INTO schema_migrations (filename) VALUES ('$name')"
done
pg "INSERT INTO workspaces (id,user_id,name,slug,env,role,members)
    SELECT 'legacy-upgrade-check',id,'Legacy','legacy','development','Owner',1 FROM users LIMIT 1"
# One old trace's raw spans will be removed, leaving only its rollup history.
ch "INSERT INTO tracium.spans (trace_id,span_id,name,start_time_ms,end_time_ms,tenant_id,cost_usd,source,schema_version)
    VALUES ('recent','one','legacy-agent',toUnixTimestamp64Milli(now64()),toUnixTimestamp64Milli(now64()),'customer',2,'span',1), ('expired','two','legacy-agent',toUnixTimestamp64Milli(now64()-INTERVAL 180 DAY),toUnixTimestamp64Milli(now64()-INTERVAL 180 DAY),'customer',3,'span',1);"
ch "ALTER TABLE tracium.spans DELETE WHERE trace_id='expired' SETTINGS mutations_sync=2"
assert_equal "$(ch 'SELECT sum(cost) FROM tracium.metrics_daily')" '5'

# No explicit ownership mapping: fail before changing the legacy schema.
if "${compose[@]}" run --rm -e LEGACY_WORKSPACE_ID= -e LEGACY_INGESTION_PAUSED=false migrate; then
  echo 'Legacy upgrade unexpectedly accepted missing ownership/maintenance settings' >&2; exit 1
fi
assert_equal "$(ch "SELECT count() FROM system.columns WHERE database='tracium' AND table='spans' AND name='tenant_id'")" '1'

"${compose[@]}" run --rm -e LEGACY_WORKSPACE_ID=legacy-upgrade-check -e LEGACY_INGESTION_PAUSED=true migrate
assert_equal "$(ch 'SELECT sum(cost) FROM tracium.metrics_daily')" '5'
assert_equal "$(ch "SELECT sum(cost) FROM tracium.metrics_daily WHERE workspace_id='legacy-upgrade-check'")" '5'
assert_equal "$(ch "SELECT count() FROM tracium.spans WHERE user_id='customer' AND workspace_id='legacy-upgrade-check'")" '1'
assert_equal "$(ch 'SELECT sum(cost) FROM tracium.metrics_daily_pre_workspace')" '5'

# Simulate a crash after data conversion but before the runner records success.
# The source backup is retained and ingestion remains paused throughout.
pg "DELETE FROM schema_migrations WHERE filename='004_workspace_upgrade.sh'"
"${compose[@]}" run --rm -e LEGACY_WORKSPACE_ID=legacy-upgrade-check -e LEGACY_INGESTION_PAUSED=true migrate
assert_equal "$(ch 'SELECT sum(cost) FROM tracium.metrics_daily')" '5'
assert_equal "$(ch "SELECT count() FROM tracium.spans WHERE workspace_id='legacy-upgrade-check'")" '1'

# The new view must resume aggregation; new unassigned spans get no old ownership.
ch "INSERT INTO tracium.spans (trace_id,span_id,name,start_time_ms,end_time_ms,user_id,cost_usd,source)
    VALUES ('new','three','new-agent',toUnixTimestamp64Milli(now64()),toUnixTimestamp64Milli(now64()),'customer',7,'span')"
assert_equal "$(ch "SELECT workspace_id = '' FROM tracium.spans WHERE trace_id='new'")" '1'
assert_equal "$(ch 'SELECT sum(cost) FROM tracium.metrics_daily')" '12'

# Old workspace owners are backfilled on the first tracked application migration.
pg "DELETE FROM workspace_members WHERE workspace_id='legacy-upgrade-check'"
assert_equal "$(pg "SELECT to_regclass('app_schema_migrations') IS NULL")" 't'
"${compose[@]}" start api
for attempt in $(seq 1 30); do
  owners=$(pg "SELECT count(*) FROM workspace_members WHERE workspace_id='legacy-upgrade-check' AND role='owner'")
  if [ "$owners" = '1' ]; then break; fi
  sleep 1
done
assert_equal "$owners" '1'
assert_equal "$(pg "SELECT count(*) FROM app_schema_migrations WHERE namespace='core' AND filename='001_identity.sql'")" '1'
echo 'PASS: legacy upgrade rejects unsafe defaults, preserves expired rollup history, resumes after interruption, and restores owner access'

# 1.0.4 -> migration 015: put the version-less rollup and views back, then upgrade.
"${compose[@]}" stop api collector
ch 'DROP VIEW tracium.metrics_daily_mv; DROP VIEW tracium.calls; DROP VIEW tracium.usage_metrics;
    DROP TABLE tracium.metrics_daily; DROP TABLE IF EXISTS tracium.metrics_daily_pre_version;
    ALTER TABLE tracium.spans DROP COLUMN workflow_version, DROP COLUMN version_source;'
for file in deploy/tests/fixtures/pre_version/*.sql collector/schema/009_create_calls_view.sql collector/schema/010_create_usage_metrics_view.sql; do
  ch "$(cat "$file")"
done
ch "INSERT INTO tracium.spans (trace_id,span_id,name,start_time_ms,end_time_ms,user_id,workspace_id,cost_usd,source)
    VALUES ('pre-version','four','old-run',toUnixTimestamp64Milli(now64()),toUnixTimestamp64Milli(now64()),'customer','legacy-upgrade-check',11,'span')"
rollup_totals='SELECT sum(cost), sum(span_count), uniqMerge(runs) FROM tracium.metrics_daily'
before=$(ch "$rollup_totals")
pg "DELETE FROM schema_migrations WHERE filename='015_workflow_version.sh'"
"${compose[@]}" run --rm migrate
assert_equal "$(ch "$rollup_totals")" "$before"
assert_equal "$(ch "SELECT count() FROM system.columns WHERE database='tracium' AND table IN ('metrics_daily','calls','usage_metrics') AND name IN ('workflow_version','version_source')")" '6'
assert_equal "$(ch "SELECT countIf(workflow_version != '') FROM tracium.metrics_daily")" '0'

# A crash after the swap, before the view is recreated, resumes from the retained table.
ch "DROP VIEW tracium.metrics_daily_mv; TRUNCATE TABLE tracium.metrics_daily"
pg "DELETE FROM schema_migrations WHERE filename='015_workflow_version.sh'"
"${compose[@]}" run --rm migrate
assert_equal "$(ch "$rollup_totals")" "$before"

# New spans roll up under their version.
ch "INSERT INTO tracium.spans (trace_id,span_id,name,start_time_ms,end_time_ms,user_id,workspace_id,cost_usd,source,workflow_version,version_source)
    VALUES ('versioned','five','new-run',toUnixTimestamp64Milli(now64()),toUnixTimestamp64Milli(now64()),'customer','legacy-upgrade-check',13,'span','v2','explicit')"
assert_equal "$(ch "SELECT sum(cost) FROM tracium.metrics_daily WHERE workflow_version='v2' AND version_source='explicit'")" '13'
"${compose[@]}" start api collector
echo 'PASS: migration 015 keeps the 1.0.4 rollup totals, resumes after interruption, and rolls up new spans by version'
