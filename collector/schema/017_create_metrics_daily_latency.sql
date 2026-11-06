-- Daily p95 run latency, a run being its root span (parent_span_id = '').
CREATE TABLE IF NOT EXISTS tracium.metrics_daily_latency (
    bucket_date      Date,
    user_id          String,
    workspace_id     String,
    workflow_name    LowCardinality(String),
    workflow_version LowCardinality(String),
    runs             SimpleAggregateFunction(sum, UInt64),
    latency          AggregateFunction(quantileTDigest(0.95), Int64)
) ENGINE = AggregatingMergeTree()
PARTITION BY toYYYYMM(bucket_date)
ORDER BY (bucket_date, user_id, workspace_id, workflow_name, workflow_version);
