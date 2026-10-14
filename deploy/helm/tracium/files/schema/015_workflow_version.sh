# Sourced by run-migrations.sh. Adds workflow_version and version_source to
# tracium.spans and the metrics_daily sorting key. Stop ingestion while it runs.

ch_query "ALTER TABLE tracium.spans
  ADD COLUMN IF NOT EXISTS workflow_version LowCardinality(String) DEFAULT '',
  ADD COLUMN IF NOT EXISTS version_source LowCardinality(String) DEFAULT ''"

rollup_needs_swap=$(ch_query "SELECT count() > 0 AND countIf(name='workflow_version') = 0 FROM system.columns WHERE database='tracium' AND table='metrics_daily'" | xargs)
if [ "$rollup_needs_swap" = "1" ]; then
  ch_query "DROP VIEW IF EXISTS tracium.metrics_daily_mv"
  ch_query "RENAME TABLE tracium.metrics_daily TO tracium.metrics_daily_pre_version"
fi

# Also resumes a copy that failed on an earlier attempt.
backup_exists=$(ch_query "EXISTS TABLE tracium.metrics_daily_pre_version" | xargs)
view_exists=$(ch_query "EXISTS TABLE tracium.metrics_daily_mv" | xargs)
if [ "$backup_exists" = "1" ] && [ "$view_exists" = "0" ]; then
  ch_query "$(cat "$MIGRATIONS_DIR/002_create_metrics_daily.sql")"
  ch_query "TRUNCATE TABLE tracium.metrics_daily"
  ch_query "INSERT INTO tracium.metrics_daily
    (bucket_date, user_id, workspace_id, workflow_name, model,
     cost, input_tokens, output_tokens, span_count, runs, error_runs)
    SELECT bucket_date, user_id, workspace_id, workflow_name, model,
           cost, input_tokens, output_tokens, span_count, runs, error_runs
    FROM tracium.metrics_daily_pre_version"
  ch_query "$(cat "$MIGRATIONS_DIR/003_create_metrics_daily_mv.sql")"
  echo "  metrics_daily rebuilt with workflow_version; original retained as metrics_daily_pre_version (safe to drop once verified)."
fi

views_have_version=$(ch_query "SELECT count() FROM system.columns WHERE database='tracium' AND table='calls' AND name='workflow_version'" | xargs)
if [ "$views_have_version" = "0" ]; then
  ch_query "DROP VIEW IF EXISTS tracium.calls"
  ch_query "$(cat "$MIGRATIONS_DIR/009_create_calls_view.sql")"
  ch_query "DROP VIEW IF EXISTS tracium.usage_metrics"
  ch_query "$(cat "$MIGRATIONS_DIR/010_create_usage_metrics_view.sql")"
  echo "  workflow_version added to tracium.calls and tracium.usage_metrics."
fi
