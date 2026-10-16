CREATE TABLE IF NOT EXISTS tracium.metrics_daily (
    bucket_date    Date,
    user_id      String,
    workspace_id String,
    workflow_name  LowCardinality(String),
    model          LowCardinality(String),
    cost           SimpleAggregateFunction(sum, Float64),
    input_tokens   SimpleAggregateFunction(sum, UInt64),
    output_tokens  SimpleAggregateFunction(sum, UInt64),
    span_count     SimpleAggregateFunction(sum, UInt64),
    runs           AggregateFunction(uniq, String),
    error_runs     AggregateFunction(uniqIf, String, UInt8)
) ENGINE = AggregatingMergeTree()
PARTITION BY toYYYYMM(bucket_date)
ORDER BY (bucket_date, user_id, workspace_id, workflow_name, model);
