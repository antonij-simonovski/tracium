# Sourced by run-migrations.sh. Adds the context-pressure, model-substitution and
# identical-output inputs to tracium.spans, and refreshes the views that select * from it.

ch_query "ALTER TABLE tracium.spans
    ADD COLUMN IF NOT EXISTS context_ratio Float32 DEFAULT 0,
    ADD COLUMN IF NOT EXISTS response_model LowCardinality(String) DEFAULT '',
    ADD COLUMN IF NOT EXISTS output_hash UInt64 DEFAULT 0,
    ADD COLUMN IF NOT EXISTS input_hash UInt64 DEFAULT 0"

views_have_column=$(ch_query "SELECT count() FROM system.columns WHERE database='tracium' AND table='calls' AND name='input_hash'" | xargs)
if [ "$views_have_column" = "0" ]; then
  ch_query "DROP VIEW IF EXISTS tracium.calls"
  ch_query "$(cat "$MIGRATIONS_DIR/009_create_calls_view.sql")"
  ch_query "DROP VIEW IF EXISTS tracium.usage_metrics"
  ch_query "$(cat "$MIGRATIONS_DIR/010_create_usage_metrics_view.sql")"
  echo "  detector columns added to tracium.calls and tracium.usage_metrics."
fi
