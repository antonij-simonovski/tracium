-- Materialized view that keeps tracium.metrics_daily_cost current.
--
-- Fires on every insert into tracium.spans, aggregates that block, and writes
-- the partial sums into metrics_daily_cost (TO target). AggregatingMergeTree
-- merges the partials per (bucket_date, user_id, workspace_id) over time.
--
-- Unlike metrics_daily_mv this view has NO `WHERE source =` filter: it splits
-- the two ingestion sources into two columns with sumIf, so one row per
-- (day, user, workspace) holds both span_cost and metric_cost. Metric rows carry
-- user_id/workspace_id (set by the collector's metric exporter) but no
-- trace/workflow/model identity — which is exactly why cost is rolled up here,
-- keyed only by the dimensions both sources share, rather than in metrics_daily.
--
-- Note: a materialized view only captures inserts made AFTER it exists. On a
-- fresh deployment there is no data yet, so nothing is missed. To adopt this on
-- a table that already holds spans, backfill once after creating it (bounded by
-- the spans TTL — older raw spans are gone, and the pre-existing metrics_daily
-- already holds their span cost):
--   INSERT INTO tracium.metrics_daily_cost
--   SELECT toDate(start_time_ms/1000), user_id, workspace_id,
--          sumIf(cost_usd, source='span'), sumIf(cost_usd, source='metric'),
--          sumIf(cost_usd, source='span' AND model='')
--   FROM tracium.spans
--   GROUP BY 1, 2, 3;
--
-- Single CREATE statement (the migration runner sends one statement per file).
CREATE MATERIALIZED VIEW IF NOT EXISTS tracium.metrics_daily_cost_mv
TO tracium.metrics_daily_cost AS
SELECT
    toDate(start_time_ms / 1000)       AS bucket_date,
    user_id,
    workspace_id,
    sumIf(cost_usd, source = 'span')                AS span_cost,
    sumIf(cost_usd, source = 'metric')              AS metric_cost,
    sumIf(cost_usd, source = 'span' AND model = '') AS reported_cost
FROM tracium.spans
GROUP BY bucket_date, user_id, workspace_id;
