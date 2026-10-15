CREATE MATERIALIZED VIEW IF NOT EXISTS tracium.metrics_daily_mv
TO tracium.metrics_daily AS
SELECT
    toDate(start_time_ms / 1000)                       AS bucket_date,
    user_id,
    workspace_id,
    workflow_name,
    if(model_normalized != '', model_normalized, model) AS model,
    sum(cost_usd)                                      AS cost,
    sum(input_tokens)                                  AS input_tokens,
    sum(output_tokens)                                 AS output_tokens,
    count()                                            AS span_count,
    uniqState(trace_id)                                AS runs,
    uniqIfState(trace_id, error_type != '' OR error_message != '') AS error_runs
FROM tracium.spans
WHERE source = 'span'
GROUP BY bucket_date, user_id, workspace_id, workflow_name, model;
