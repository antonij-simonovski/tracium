# Repairing the daily rollup

Removing a bad span from `tracium.spans` is **not** enough to correct the
dashboard. Remediation must clean **both** tables.

## Why deleting the span is not enough

`tracium.metrics_daily` is filled by `metrics_daily_mv`, an **insert trigger**.
Materialized views do not retract on `DELETE`, so whatever a bad span
contributed to the rollup stays there after the span itself is gone.

This is easy to miss, because the two halves of the product disagree:

| Dashboard window | Reads from | After deleting the span |
|---|---|---|
| 24h / 7d / 30d | raw `tracium.spans` | recovers immediately |
| 90d / 1y | `tracium.metrics_daily` | **stays wrong forever** |

An operator deletes the offending span, watches the short windows return to
normal, and concludes the incident is over. The long windows keep serving the
poisoned figure with no visible cause.

```sql
ALTER TABLE tracium.spans DELETE WHERE name = 'huge.tokens';  -- 0 rows remain
SELECT cost FROM tracium.metrics_daily WHERE user_id = 'affected-user';
-- 3458764513820.541   ← still there
```

## The repair

The `repair-rollup` command rebuilds specific `bucket_date` days from the raw
spans, using the same aggregation each materialized view performs. It ships in
the API image (`ghcr.io/antonij-simonovski/api`) and rebuilds **both** rollups
(`tracium.metrics_daily` and `tracium.metrics_daily_cost`) in one run.

It connects over ClickHouse's native protocol via `CLICKHOUSE_DSN` (the same
value the API uses), so run it in-cluster where port 9000 is reachable, e.g. a
one-off pod from the API image, or `docker compose run`:

```bash
# 1. Remove the bad spans from the raw table.
clickhouse-client --query "ALTER TABLE tracium.spans DELETE WHERE <predicate>"

# 2. Inspect what the rebuild would do (changes nothing).
docker compose run --rm --entrypoint ./repair-rollup api \
  --from 2026-07-14 --to 2026-07-16 --dry-run

# 3. Rebuild those days.
docker compose run --rm --entrypoint ./repair-rollup api \
  --from 2026-07-14 --to 2026-07-16 --yes
```

Step 1 must complete before step 2. The rebuild reads whatever is in
`tracium.spans` at the time it runs, so a span still present is faithfully
re-aggregated back into the rollup.

Flags: `--from`/`--to` (inclusive day range), `--dry-run`, `--allow-empty`,
`--force` (skip the quiescence guard; only with ingestion stopped), `--yes`
(no confirmation prompt), `--quiesce-seconds` (default 300). Run with `-h` for
the full list.

## What the command protects you from

It refuses to run rather than write aggregates it cannot vouch for:

- **Schema drift.** Migrations use `CREATE ... IF NOT EXISTS`, so a long-lived
  deployment's objects can differ from the schema files. On every run the command
  normalises each rollup's materialized-view SELECT (via `EXPLAIN SYNTAX`) and
  compares it against the aggregation it mirrors internally, for **both**
  `metrics_daily` and `metrics_daily_cost`, and aborts on any mismatch, so it
  never writes aggregates the live trigger would not have produced. (This guard,
  and the tool itself, are unit-tested in `api/internal/rolluprepair`.)
- **Still-open days.** Rebuilding a day that is still receiving spans
  double-counts them: the view inserts a partial for each new span and the
  rebuild reads it too. Days with recent arrivals are refused unless you stop
  ingestion and pass `--force`. Late spans arriving *after* a rebuild are fine:
  the view adds them on top, exactly as it would have.
- **Aged-out days.** If a day's raw spans have already expired under the spans
  TTL, rebuilding would replace real history with an empty day, since the rollups
  deliberately outlive raw spans. Such days are skipped unless you pass
  `--allow-empty`.

## Cost

Bounded by the days you request, never by the table size: the span read is a
half-open `start_time_ms` range that prunes on the sort key. There is no
"rebuild everything" mode; at Tracium's scale bar that would be unrunnable.

## Prevention

Ingest-side bounds (token ceilings, the $10,000 cap on a reported
`tracium.cost_usd`, and ignoring vendor cost attributes on model calls) stop this class of poison from being priced and stored in
the first place. This command is for cleaning up damage already done.
