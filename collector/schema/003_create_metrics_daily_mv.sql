-- Materialized view that keeps tracium.metrics_daily current.
--
-- Fires on every insert into tracium.spans, aggregates that block, and writes
-- the partial states into metrics_daily (TO target). AggregatingMergeTree merges
-- the partials per (bucket_date, user_id, workspace_id, workflow_name,
-- workflow_version, version_source, model)
-- over time.
--
-- source='span' only: aggregate token-usage metric rows (source='metric') carry
-- no trace identity and would distort run counts, so the rollup is span-derived
-- (cost included). model prefers the normalized id; workflow_name is the span's
-- collector-derived workflow (the resource service.name for a trace's spans).
--
-- Note: a materialized view only captures inserts made AFTER it exists. On a
-- fresh deployment there is no data yet, so nothing is missed. To adopt this on
-- a table that already holds spans, backfill once after creating it:
--   INSERT INTO tracium.metrics_daily
--   SELECT toDate(start_time_ms/1000), user_id, workspace_id, workflow_name,
--          workflow_version, version_source,
--          if(model_normalized!='', model_normalized, model),
--          sum(cost_usd), sum(input_tokens), sum(output_tokens), count(),
--          uniqState(trace_id),
--          uniqIfState(trace_id, error_type!='' OR error_message!='')
--   FROM tracium.spans WHERE source='span'
--   GROUP BY 1,2,3,4,5,6,7;
--
-- Single CREATE statement (the migration runner sends one statement per file).
CREATE MATERIALIZED VIEW IF NOT EXISTS tracium.metrics_daily_mv
TO tracium.metrics_daily AS
SELECT
    toDate(start_time_ms / 1000)                       AS bucket_date,
    user_id,
    workspace_id,
    workflow_name,
    workflow_version,
    version_source,
    if(model_normalized != '', model_normalized, model) AS model,
    sum(cost_usd)                                      AS cost,
    sum(input_tokens)                                  AS input_tokens,
    sum(output_tokens)                                 AS output_tokens,
    count()                                            AS span_count,
    uniqState(trace_id)                                AS runs,
    uniqIfState(trace_id, error_type != '' OR error_message != '') AS error_runs
FROM tracium.spans
WHERE source = 'span'
GROUP BY bucket_date, user_id, workspace_id, workflow_name, workflow_version, version_source, model;
