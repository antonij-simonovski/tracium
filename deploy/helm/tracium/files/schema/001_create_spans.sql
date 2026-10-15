-- Tracium span store.
--
-- One row per OTLP span (source='span') or per token-usage metric point rolled
-- into a span-shaped row (source='metric'). All dashboard reads aggregate this
-- table, so its physical layout is chosen for how the API queries it:
--
--   * ORDER BY (start_time_ms, trace_id) — every dashboard query bounds a time
--     window (last 24h/7d/30d). A time-leading sort key lets ClickHouse skip the
--     granules outside the window, so a query reads ~the rows in its window
--     rather than the whole month partition. trace_id second clusters a trace's
--     spans together within a time slice.
--   * INDEX idx_trace_id — single-trace reads (GetTrace/GetSpans) look up by
--     trace_id with no time bound. Without a skip index that is a full scan, since
--     trace_id is not the leading key. A bloom filter prunes to the few granules
--     that hold the trace.
--     The false-positive rate here is load-bearing, not a tuning detail: it is the
--     ONLY thing bounding a query that has no time bound, so the granules read are
--     ~fp x (whole table). At fp=0.01 a 5-span lookup read 40,960 rows at 5M rows,
--     90,112 at 10M and 124,001 at 15M — linear in table size, which breaks the
--     cardinal rule (cost must track the query window, never total rows stored).
--     At fp=0.001 the same lookup pins to one granule: 8,192 rows flat at 5M/10M/
--     15M, and the index is smaller on disk compressed (1.80 vs 3.60 MiB at 5M),
--     because the sparser filter has more zero runs. Do not raise it back.
--   * INDEX idx_user — the user_id business filter lets user-scoped reads
--     skip granules with no matching client. It keeps fp=0.01 deliberately: unlike
--     trace_id, user_id is never queried on its own — every user predicate is
--     appended to a clause already bounded by start_time_ms, so the time-leading
--     sort key does the pruning and those reads already measure flat as the table
--     grows. user_id is also low-cardinality (a handful of end-client labels),
--     so most granules hold most users and no false-positive rate makes them
--     skippable. Tightening it would cost index bits for no measured gain.
--   * TTL — caps retention so the table does not grow without bound. The 90 below
--     is the default; the migration runner rewrites it from RETENTION_DAYS at
--     apply time (RETENTION_DAYS=0 drops the TTL entirely, keeping data forever —
--     e.g. for annual reporting). To change retention on an already-created table,
--     run: ALTER TABLE tracium.spans MODIFY TTL toDateTime(start_time_ms/1000) +
--     INTERVAL <days> DAY  (or MODIFY TTL ... REMOVE / no expr to clear it).
--
-- Single CREATE statement (the migration runner sends one statement per file).
--
-- !! EXISTING DEPLOYMENTS DO NOT ADOPT INDEX CHANGES !!
-- idx_trace_id changed from bloom_filter(0.01) to bloom_filter(0.001). Because
-- this is CREATE TABLE IF NOT EXISTS, the statement is a silent no-op wherever
-- tracium.spans already exists — the table keeps its old 0.01 index and single-
-- trace reads there stay linear in table size. Only fresh volumes get the fix.
-- To adopt it on a live table, rebuild the index in place:
--   ALTER TABLE tracium.spans DROP INDEX idx_trace_id;
--   ALTER TABLE tracium.spans
--     ADD INDEX idx_trace_id trace_id TYPE bloom_filter(0.001) GRANULARITY 1;
--   ALTER TABLE tracium.spans MATERIALIZE INDEX idx_trace_id;  -- rewrites parts
CREATE TABLE IF NOT EXISTS tracium.spans (
    trace_id          String,
    span_id           String,
    parent_span_id    String,
    name              String,
    start_time_ms     Int64,
    end_time_ms       Int64,
    duration_ms       Int64,
    model             String,
    model_normalized  String,
    input_tokens      Int64,
    output_tokens     Int64,
    cost_usd          Float64,
    user_id         String,
    workspace_id    String,
    finish_reason     String,
    error_type        String,
    error_message     String,
    schema_version    Int32,
    received_at       DateTime DEFAULT now(),
    input             String,
    output            String,
    available_tools   String,
    source            LowCardinality(String) DEFAULT 'span',
    workflow_name     LowCardinality(String) DEFAULT '',
    -- Resource-level service.name, kept verbatim ("unknown_service*" stored as '').
    -- workflow_name now prefers span-scoped signals (gen_ai.agent.name, traceloop
    -- entity/workflow) so a trace made of many sub-spans attributes each span to
    -- its real workflow; service_name preserves the always-present resource name
    -- the query layer falls back to for a trace's in-flight display name. Additive column —
    -- per Rule 5 it does not bump schema_version, and existing tables do not adopt
    -- it (CREATE IF NOT EXISTS); add it in place with:
    --   ALTER TABLE tracium.spans ADD COLUMN IF NOT EXISTS
    --     service_name LowCardinality(String) DEFAULT '';
    service_name      LowCardinality(String) DEFAULT '',
    -- Added to existing tables by migration 014.
    workflow_version  LowCardinality(String) DEFAULT '',
    kind              LowCardinality(String) DEFAULT '',
    -- Metering provenance. output_tokens_derived: part of output_tokens was
    -- reconciled from total_tokens rather than reported by the provider (Gemini
    -- thinking tokens). unmetered: the call returned output but carried no usage
    -- at all (a stream without include_usage), so its $0 is unknown, not free.
    -- Additive columns — per Rule 5 an additive change does not bump
    -- schema_version. Existing deployments do not adopt them (see the note
    -- above); add them in place with:
    --   ALTER TABLE tracium.spans ADD COLUMN IF NOT EXISTS
    --     output_tokens_derived UInt8 DEFAULT 0, ADD COLUMN IF NOT EXISTS
    --     unmetered UInt8 DEFAULT 0;
    output_tokens_derived UInt8 DEFAULT 0,
    unmetered             UInt8 DEFAULT 0,
    -- Custom business attributes the instrumentation attached and the operator
    -- allocates by (team, user.id, environment, customer, cost_center, …), stored
    -- as a Map so the query layer can GROUP BY / filter attributes['<key>'] with
    -- no schema change. Additive column — per Rule 5 it does not bump
    -- schema_version, and existing tables do not adopt it (CREATE IF NOT EXISTS);
    -- add it in place with:
    --   ALTER TABLE tracium.spans ADD COLUMN IF NOT EXISTS
    --     attributes Map(String, String) DEFAULT map();
    attributes Map(String, String) DEFAULT map(),
    INDEX idx_trace_id trace_id  TYPE bloom_filter(0.001) GRANULARITY 1,
    INDEX idx_user   user_id TYPE bloom_filter(0.01) GRANULARITY 4,
    INDEX idx_workspace workspace_id TYPE bloom_filter(0.01) GRANULARITY 4
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(toDateTime(start_time_ms / 1000))
ORDER BY (start_time_ms, trace_id)
TTL toDateTime(start_time_ms / 1000) + INTERVAL 90 DAY;
