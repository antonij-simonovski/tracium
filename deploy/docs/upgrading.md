# Upgrade notes

## Workspace-scoped telemetry

Fresh installations run all migrations automatically. Existing installations
whose spans still contain `tenant_id` must explicitly choose an existing
workspace to own their historical telemetry. `tenant_id` was a business label;
it cannot safely identify an account or establish ownership.

The upgrade retains raw spans, user labels, and the old daily aggregate states,
including history whose raw spans have expired. It retains the original rollup
as `tracium.metrics_daily_pre_workspace`. Back up ClickHouse and Postgres before
upgrading and keep the old images until validation completes.

### Docker Compose

1. Using the existing dashboard, create or select the workspace that should own
   **all legacy telemetry**. Record its ID and owner. If old telemetry belongs to
   several separately authorized groups, do not use this single-workspace
   migration until an operator has separated that data with an explicit mapping.
2. Pause application exporters and drain the collector queue. Stop the old API
   and collector: `docker compose stop api collector`. Leave both databases up.
3. Add these values to `.env`:

   ```dotenv
   LEGACY_WORKSPACE_ID=THE_EXISTING_WORKSPACE_ID
   LEGACY_INGESTION_PAUSED=true
   ```

4. Check out the new release's source (migrations run from its
   `collector/schema/`), set `TRACIUM_VERSION` in `.env` to that release, run
   `docker compose pull`, then `docker compose run --rm migrate`. On failure, leave ingestion stopped and
   rerun the migration with the **same workspace ID**. The migration checks its
   saved mapping and rebuilds the new rollup from the retained old states so a
   retry does not duplicate totals.
5. Start the new services with `docker compose up -d`. Existing workspace owners
   receive owner memberships automatically. Sign in as the chosen owner and
   verify both recent traces and long-range totals before resuming exporters.
6. Create an API key for the workspace (its **API keys** screen) and configure
   every exporter to send it as `Authorization: Bearer <key>`. The key decides
   the workspace, so exporters set no workspace attribute. Remove the two
   `LEGACY_*` settings and resume ingestion.

Do not restart the old collector after migrating: it still writes `tenant_id`.
Rollback requires restoring the pre-upgrade databases and old images together.
Do not drop the retained rollup until the upgrade has been verified.

### Kubernetes

Pause producers, drain queues, and scale the old collector and API to zero
before upgrading. Pass `migrate.legacyWorkspaceId` and
`migrate.legacyIngestionPaused=true` to Helm. Keep ingestion paused throughout
the pre-upgrade migration hook, including any retry. The same ownership and
backup requirements apply. Remove these values after successful validation.

The migration script runs only once according to Postgres `schema_migrations`.
Do not delete migration records or manually rerun completed upgrade scripts
against an active installation.

## Backfilling the reconciled cost rollup

Migrations `007`/`008` add `tracium.metrics_daily_cost`, a source-aware cost
rollup that lets long windows (90d/1y) reconcile span- and metric-derived spend
the same way short windows do. Before it existed, the long-window cost read
`tracium.metrics_daily`, which is span-only, so a wide range could report less
cost than a shorter one whenever the metric source metered more than spans.

The table and its materialized view are created automatically by the migration
Job (Kubernetes) / `migrate` service (Compose). **No action is needed on a fresh
install.** On an existing install the view only captures spans inserted *after*
it exists, so backfill the days already in `tracium.spans` once, after the
migration has run:

```sql
INSERT INTO tracium.metrics_daily_cost
SELECT toDate(start_time_ms/1000), user_id, workspace_id,
       sumIf(cost_usd, source='span'), sumIf(cost_usd, source='metric')
FROM tracium.spans
GROUP BY 1, 2, 3;
```

Run it once, while it is safe to read the current spans (ideally with ingestion
quiet, so a day is not both backfilled here and captured by the view, the same
open-day caution as `rollup-repair.md`). It only recovers days still within the
spans TTL; older days keep whatever span cost the pre-existing `metrics_daily`
already holds, and long windows fall back to that for those days.

Until the backfill runs, long-window cost reads an empty `metrics_daily_cost`
and reports 0 for the affected days rather than a wrong figure. Backfill before
directing users at long-range cost if that transient is unacceptable.

> Note: because materialized views do not retract on DELETE, a bad span deleted
> from `tracium.spans` leaves its contribution in both `metrics_daily` and
> `metrics_daily_cost`. The `repair-rollup` command (in the API image) rebuilds
> **both** rollups for the affected days; see `rollup-repair.md`.

## Typed read views (calls / usage_metrics)

Migrations `009`/`010` add two views over `tracium.spans`:
`tracium.calls` (`source='span'`) and `tracium.usage_metrics` (`source='metric'`).
The API reads these instead of filtering `source` itself, so a trace/workflow/latency
query structurally cannot see identity-less metric rows, and cost reconciliation
reads both views explicitly. They hold no data and copy nothing: reading a view
is rewritten to its underlying `SELECT` at run time.

**The new API depends on these views existing.** They are created by the same
migration step that runs before the app in every deploy path (the Helm migrate
hook is `pre-upgrade`; Compose runs `migrate` before `up`), so the standard order
already covers it; just don't roll the new API pods against a database that has
not yet applied `009`/`010`. Nothing to backfill: the views are pure metadata.

If you later `ADD COLUMN` to `tracium.spans` and the API must read it, recreate
the views (they are `CREATE VIEW IF NOT EXISTS`, so a re-run alone is a no-op):
`DROP VIEW IF EXISTS tracium.calls;` then re-apply the migration, likewise for
`tracium.usage_metrics`.

## Workflow versions (migration 014)

Migration `014` adds `workflow_version` to `tracium.spans` and to the
`tracium.metrics_daily` sorting key. A sorting key can't gain a column
in place, so the migration renames the rollup to
`tracium.metrics_daily_pre_version`, recreates it, and copies every aggregate
state across as unversioned. It also recreates the `calls` and `usage_metrics`
views so they expose the new column.

Stop the collector while it runs: spans written while the rollup's view is
dropped are not added to `metrics_daily`. If it fails, rerun it; a retry picks
up from the retained table. The new API reads the new column, so apply the
migration before rolling the API (the standard deploy order already does). Drop
`tracium.metrics_daily_pre_version` once long-range totals check out.

Spans stored before the upgrade have no version. Only spans ingested afterwards
carry one.
