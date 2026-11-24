# Sourced by run-migrations.sh. Adds the ingest silent-failure verdict and cache
# reads to tracium.spans, and refreshes the views that select * from it.

ch_query "ALTER TABLE tracium.spans
    ADD COLUMN IF NOT EXISTS cache_read_tokens Int64 DEFAULT 0,
    ADD COLUMN IF NOT EXISTS silent_failure LowCardinality(String) DEFAULT ''"

views_have_column=$(ch_query "SELECT count() FROM system.columns WHERE database='tracium' AND table='calls' AND name='silent_failure'" | xargs)
if [ "$views_have_column" = "0" ]; then
  ch_query "DROP VIEW IF EXISTS tracium.calls"
  ch_query "$(cat "$MIGRATIONS_DIR/009_create_calls_view.sql")"
  ch_query "DROP VIEW IF EXISTS tracium.usage_metrics"
  ch_query "$(cat "$MIGRATIONS_DIR/010_create_usage_metrics_view.sql")"
  echo "  silent_failure and cache_read_tokens added to tracium.calls and tracium.usage_metrics."
fi
