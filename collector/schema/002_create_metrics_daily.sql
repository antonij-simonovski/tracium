-- Daily metrics rollup.
--
-- Long-horizon dashboards (90d, 1y, …) cannot afford to scan raw spans: a
-- year-wide query would read every span in the year. This pre-aggregated table
-- collapses spans to one row per (day, user, workflow, version, model), so a long-range
-- query reads ~(days × dimension cardinality) rows instead of N spans — its size
-- tracks cardinality, not span volume. Raw spans keep their short TTL for
-- drill-down; this table is tiny and is retained for years.
--
-- It is keyed by bucket_date — a deterministic per-row value (toDate of the span
-- start), NOT an aggregate — so it time-prunes on the partition/sort key. (A
-- per-trace summary cannot: an aggregate like min(start_time_ms) is rejected in
-- the sort key, so it could not time-prune. That is why the rollup is bucketed.)
--
-- Aggregate columns:
--   * Additive sums (cost, tokens, span_count) use SimpleAggregateFunction — the
--     stored value is the final value; querying just sums across buckets.
--   * runs / error_runs are distinct trace counts and need merge state:
--     uniq(trace_id) and uniqIf(trace_id, errored). uniq is approximate
--     (HyperLogLog, ~1%) — fine for dashboards/trends. Querying uses uniqMerge.
--     uniqIf(trace_id, span_errored) reproduces the product's trace-error rule
--     (a trace errored iff any of its spans errored).
--
-- The companion migration 003 creates the materialized view that fills this.
-- Single CREATE statement (the migration runner sends one statement per file).
CREATE TABLE IF NOT EXISTS tracium.metrics_daily (
    bucket_date    Date,
    user_id      String,
    workspace_id String,
    workflow_name  LowCardinality(String),
    workflow_version LowCardinality(String),
    model          LowCardinality(String),
    cost           SimpleAggregateFunction(sum, Float64),
    input_tokens   SimpleAggregateFunction(sum, UInt64),
    output_tokens  SimpleAggregateFunction(sum, UInt64),
    span_count     SimpleAggregateFunction(sum, UInt64),
    runs           AggregateFunction(uniq, String),
    error_runs     AggregateFunction(uniqIf, String, UInt8)
) ENGINE = AggregatingMergeTree()
PARTITION BY toYYYYMM(bucket_date)
ORDER BY (bucket_date, user_id, workspace_id, workflow_name, workflow_version, model);
