# Sourced by run-migrations.sh. Fresh databases already have the new schema.
# Upgrades preserve all legacy rollup states, including expired raw-span history.
# Ingestion MUST be stopped for the entire upgrade and any retry.
legacy_columns=$(ch_query "SELECT count() FROM system.columns WHERE database='tracium' AND table='spans' AND name='tenant_id'")
psql "$POSTGRES_DSN" -v ON_ERROR_STOP=1 -q -c "
  CREATE TABLE IF NOT EXISTS workspace_upgrade_state (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    workspace_id TEXT NOT NULL
  )"
upgrade_workspace=$(psql "$POSTGRES_DSN" -tAc "SELECT workspace_id FROM workspace_upgrade_state WHERE singleton")
if [ "$legacy_columns" = "0" ] && [ -z "$upgrade_workspace" ]; then
  echo "  Workspace schema already current."
else
  if [ "${LEGACY_INGESTION_PAUSED:-false}" != "true" ]; then
    echo 'Stop the API and collector, then set LEGACY_INGESTION_PAUSED=true. See deploy/docs/upgrading.md.' >&2
    exit 1
  fi
  requested_workspace="${LEGACY_WORKSPACE_ID:-}"
  if ! [[ "$requested_workspace" =~ ^[a-zA-Z0-9_-]{1,64}$ ]]; then
    echo 'Set LEGACY_WORKSPACE_ID to the existing workspace that should own ALL legacy telemetry. See deploy/docs/upgrading.md.' >&2
    exit 1
  fi
  if [ -n "$upgrade_workspace" ] && [ "$upgrade_workspace" != "$requested_workspace" ]; then
    echo 'A workspace upgrade is already in progress for a different workspace. Keep the original mapping when retrying.' >&2
    exit 1
  fi
  owner_exists=$(psql "$POSTGRES_DSN" -v ON_ERROR_STOP=1 -tAc "SELECT count(*) FROM workspaces WHERE id='$requested_workspace'")
  if [ "$owner_exists" != "1" ]; then
    echo 'LEGACY_WORKSPACE_ID must name an existing workspace; no ownership is inferred from telemetry labels.' >&2
    exit 1
  fi
  psql "$POSTGRES_DSN" -v ON_ERROR_STOP=1 -q -c "INSERT INTO workspace_upgrade_state (workspace_id) VALUES ('$requested_workspace') ON CONFLICT DO NOTHING"

  ch_query "DROP VIEW IF EXISTS tracium.metrics_daily_mv"
  ch_query "ALTER TABLE tracium.spans RENAME COLUMN IF EXISTS tenant_id TO user_id"
  # The owning-identity column was renamed agent_name -> workflow_name (migration
  # 011). This block recreates metrics_daily and its MV from the current canonical
  # DDL, which selects workflow_name, so the spans column must already carry that
  # name here — 011 runs later. Rename it inline, exactly as tenant_id above.
  ch_query "ALTER TABLE tracium.spans RENAME COLUMN IF EXISTS agent_name TO workflow_name"
  ch_query "ALTER TABLE tracium.spans ADD COLUMN IF NOT EXISTS workspace_id String DEFAULT ''"
  # The rollup DDL reads the column migration 014 adds.
  ch_query "ALTER TABLE tracium.spans ADD COLUMN IF NOT EXISTS workflow_version LowCardinality(String) DEFAULT ''"
  # Store ownership explicitly. A permanent legacy default would silently grant
  # the old workspace access to future untagged spans. UPDATE is retry-safe
  # while ingestion is paused and does not overwrite already assigned rows.
  ch_query "ALTER TABLE tracium.spans MODIFY COLUMN workspace_id String DEFAULT ''"
  ch_query "ALTER TABLE tracium.spans UPDATE workspace_id='$requested_workspace' WHERE workspace_id='' SETTINGS mutations_sync=2"

  # The old rollup's sorting key cannot be renamed in place. Preserve it intact
  # and copy its aggregate states to the new layout. Retrying never appends twice.
  old_rollup=$(ch_query "SELECT count() FROM system.columns WHERE database='tracium' AND table='metrics_daily' AND name='tenant_id'")
  if [ "$old_rollup" != "0" ]; then
    ch_query "RENAME TABLE tracium.metrics_daily TO tracium.metrics_daily_pre_workspace"
  fi
  backup_exists=$(ch_query "EXISTS TABLE tracium.metrics_daily_pre_workspace")
  if [ "$backup_exists" != "1" ]; then
    echo 'Legacy rollup backup is missing; refusing to replace aggregates.' >&2
    exit 1
  fi
  ch_query "$(cat "$MIGRATIONS_DIR/002_create_metrics_daily.sql")"
  ch_query "TRUNCATE TABLE tracium.metrics_daily"
  ch_query "INSERT INTO tracium.metrics_daily
    (bucket_date, user_id, workspace_id, workflow_name, model,
     cost, input_tokens, output_tokens, span_count, runs, error_runs)
    SELECT bucket_date, tenant_id, '$requested_workspace', agent_name, model,
           cost, input_tokens, output_tokens, span_count, runs, error_runs
    FROM tracium.metrics_daily_pre_workspace"
  ch_query "$(cat "$MIGRATIONS_DIR/003_create_metrics_daily_mv.sql")"
  echo "  Legacy telemetry assigned to $requested_workspace; original rollup retained as metrics_daily_pre_workspace."
fi
