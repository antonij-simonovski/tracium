-- Daily cost rollup, reconciled across ingestion sources.
--
-- The main rollup (tracium.metrics_daily, migrations 002/003) is span-derived
-- only: metric-source rows carry no trace/workflow/model identity, so folding
-- them into that table would distort run counts and the workflow/model dimensions. But
-- cost IS metered by both sources, and the short-window (raw-span) cost path
-- reconciles them per bucket with greatest(span, metric) — each source is a
-- lower bound on true spend, so the greater is the tightest non-double-counting
-- estimate (see costReconcileExpr in api/internal/query/metrics.go). Without a
-- source-aware cost rollup, long windows (90d/1y) count span cost only and can
-- report LESS spend than a shorter, raw-span window over the same days.
--
-- This table carries cost split by source, keyed only by the dimensions metric
-- rows actually have — (bucket_date, user_id, workspace_id) — so metric rows fit
-- cleanly. The API reconciles at read time: per day, greatest(span_cost,
-- metric_cost), summed over the window. That is the day-grain analog of the raw
-- path's per-bucket reconciliation, and when no metrics are ingested every
-- metric_cost is 0, so greatest(span, 0) = span and the result is identical to
-- the span-only rollup. reported_cost is the part of span_cost from spans with
-- no model (a sender's own tracium.cost_usd); metrics never meter it, so it is
-- added outside the greatest. Migration 015 adds it to existing installs.
--
-- Additive sums use SimpleAggregateFunction(sum): the stored value is final, and
-- a query just sums across merged parts. There is no run/uniq state here — cost
-- is additive, and run counts stay in metrics_daily.
--
-- Like metrics_daily this table is tiny (size tracks day × user × workspace
-- cardinality, not span volume) and carries no TTL, so long-horizon cost stays
-- available after the raw spans have aged out.
--
-- The companion migration 008 creates the materialized view that fills this.
-- Single CREATE statement (the migration runner sends one statement per file).
CREATE TABLE IF NOT EXISTS tracium.metrics_daily_cost (
    bucket_date  Date,
    user_id      String,
    workspace_id String,
    span_cost     SimpleAggregateFunction(sum, Float64),
    metric_cost   SimpleAggregateFunction(sum, Float64),
    reported_cost SimpleAggregateFunction(sum, Float64)
) ENGINE = AggregatingMergeTree()
PARTITION BY toYYYYMM(bucket_date)
ORDER BY (bucket_date, user_id, workspace_id);
