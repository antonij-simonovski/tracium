CREATE MATERIALIZED VIEW IF NOT EXISTS tracium.metrics_daily_latency_mv
TO tracium.metrics_daily_latency AS
SELECT
    toDate(start_time_ms / 1000)          AS bucket_date,
    user_id,
    workspace_id,
    workflow_name,
    workflow_version,
    count()                               AS runs,
    quantileTDigestState(0.95)(duration_ms) AS latency
FROM tracium.spans
WHERE source = 'span' AND parent_span_id = ''
GROUP BY bucket_date, user_id, workspace_id, workflow_name, workflow_version;
