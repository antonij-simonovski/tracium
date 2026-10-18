-- Spans the collector rejected, counted per workspace, hour and error code.
CREATE TABLE IF NOT EXISTS tracium.rejected_spans (
    workspace_id String,
    hour         DateTime DEFAULT toStartOfHour(now()),
    code         LowCardinality(String),
    spans        UInt64
) ENGINE = SummingMergeTree(spans)
PARTITION BY toYYYYMM(hour)
ORDER BY (workspace_id, hour, code);
