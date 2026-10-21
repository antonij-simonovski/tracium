# Sourced by run-migrations.sh. Adds reported_cost (tracium.cost_usd on spans
# with no model) to the daily cost rollup. Stop ingestion while it runs.

ch_query "ALTER TABLE tracium.metrics_daily_cost ADD COLUMN IF NOT EXISTS reported_cost SimpleAggregateFunction(sum, Float64)"

view_has_column=$(ch_query "SELECT count() FROM system.columns WHERE database='tracium' AND table='metrics_daily_cost_mv' AND name='reported_cost'" | xargs)
if [ "$view_has_column" = "0" ]; then
  ch_query "DROP VIEW IF EXISTS tracium.metrics_daily_cost_mv"
  ch_query "$(cat "$MIGRATIONS_DIR/008_create_metrics_daily_cost_mv.sql")"
  echo "  metrics_daily_cost_mv recreated with reported_cost."
fi
