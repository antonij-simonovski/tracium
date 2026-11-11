-- Spans per hour for a keyed dimension (dim = 'client': key is user_id).
CREATE TABLE IF NOT EXISTS tracium.activity_hourly (
    workspace_id String,
    dim          LowCardinality(String),
    key          String,
    hour         DateTime,
    spans        UInt64
) ENGINE = SummingMergeTree(spans)
PARTITION BY toYYYYMM(hour)
ORDER BY (workspace_id, dim, key, hour);
