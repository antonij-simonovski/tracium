# Sourced by run-migrations.sh. Renames the collector-derived owning-identity
# column agent_name → workflow_name across the span store and the daily rollup.
#
# The dimension was mislabelled "agent": it is the workflow a trace is grouped
# under (derived from service.name / traceloop.workflow.name / gen_ai.agent.name /
# the span name), not an agent in the gen_ai sense. The span `kind` value "agent"
# and the source attribute gen_ai.agent.name are genuine agent concepts and are
# left untouched.
#
# Fresh databases already have workflow_name (migrations 001–003 create it), so
# this is a no-op there. On an existing install the rename is applied in place.
# Ingestion SHOULD be stopped for the rebuild: while the rollup MV is dropped and
# recreated, spans inserted in the gap are not folded into metrics_daily.
#
# tracium.spans.workflow_name is a plain column → ALTER RENAME COLUMN.
# tracium.metrics_daily.workflow_name is part of the sorting key → it cannot be
# renamed in place, so the table is swapped and its aggregate states copied
# positionally into the new layout (same technique as migration 004).

spans_has_legacy=$(ch_query "SELECT count() FROM system.columns WHERE database='tracium' AND table='spans' AND name='agent_name'" | xargs)
rollup_has_legacy=$(ch_query "SELECT count() FROM system.columns WHERE database='tracium' AND table='metrics_daily' AND name='agent_name'" | xargs)

if [ "$spans_has_legacy" = "0" ] && [ "$rollup_has_legacy" = "0" ]; then
  echo "  agent_name already renamed to workflow_name; nothing to do."
else
  # The MV reads spans.agent_name, so it must go before the column is renamed.
  ch_query "DROP VIEW IF EXISTS tracium.metrics_daily_mv"

  if [ "$spans_has_legacy" != "0" ]; then
    ch_query "ALTER TABLE tracium.spans RENAME COLUMN agent_name TO workflow_name"
  fi
  # The rollup DDL reads the columns migration 015 adds.
  ch_query "ALTER TABLE tracium.spans
    ADD COLUMN IF NOT EXISTS workflow_version LowCardinality(String) DEFAULT '',
    ADD COLUMN IF NOT EXISTS version_source LowCardinality(String) DEFAULT ''"

  if [ "$rollup_has_legacy" != "0" ]; then
    # Preserve the old rollup intact, recreate it with the renamed sort-key
    # column, then copy every aggregate state across.
    ch_query "RENAME TABLE tracium.metrics_daily TO tracium.metrics_daily_pre_workflow_rename"
    ch_query "$(cat "$MIGRATIONS_DIR/002_create_metrics_daily.sql")"
    ch_query "TRUNCATE TABLE tracium.metrics_daily"
    ch_query "INSERT INTO tracium.metrics_daily
      (bucket_date, user_id, workspace_id, workflow_name, model,
       cost, input_tokens, output_tokens, span_count, runs, error_runs)
      SELECT * FROM tracium.metrics_daily_pre_workflow_rename"
    echo "  metrics_daily rebuilt with workflow_name; original retained as metrics_daily_pre_workflow_rename (safe to drop once verified)."
  fi

  # Recreate the MV from the current DDL (now selects workflow_name from spans).
  ch_query "$(cat "$MIGRATIONS_DIR/003_create_metrics_daily_mv.sql")"

  # The calls / usage_metrics views are `SELECT *`, but ClickHouse freezes the
  # expanded column list into the view definition at creation time — so after the
  # column rename they still expose the old `agent_name` and every read through
  # them fails. Recreate both from their DDL to re-expand `*` over the new columns.
  ch_query "DROP VIEW IF EXISTS tracium.calls"
  ch_query "$(cat "$MIGRATIONS_DIR/009_create_calls_view.sql")"
  ch_query "DROP VIEW IF EXISTS tracium.usage_metrics"
  ch_query "$(cat "$MIGRATIONS_DIR/010_create_usage_metrics_view.sql")"
  echo "  agent_name renamed to workflow_name."
fi
