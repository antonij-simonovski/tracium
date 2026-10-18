#!/bin/bash
# Tracium schema migrations — idempotent, safe to re-run.
# Uses the ClickHouse HTTP API (port 8123) so no clickhouse-client binary needed.
set -euo pipefail

CLICKHOUSE_HTTP="${CLICKHOUSE_HTTP:-http://clickhouse:8123}"
CLICKHOUSE_PASSWORD="${CLICKHOUSE_PASSWORD:?CLICKHOUSE_PASSWORD is required}"
POSTGRES_DSN="${POSTGRES_DSN:?POSTGRES_DSN is required}"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-/migrations}"

# Span retention, in days. Substituted into the spans schema's TTL clause at
# apply time so operators can keep data as long as they need (e.g. for annual
# reporting) without editing SQL. 0 (or empty) keeps data forever — the TTL
# clause is dropped entirely. Defaults to 90 days.
RETENTION_DAYS="${RETENTION_DAYS:-90}"
if ! printf '%s' "${RETENTION_DAYS}" | grep -Eq '^[0-9]+$'; then
  echo "error: RETENTION_DAYS must be a non-negative integer (got '${RETENTION_DAYS}')" >&2
  exit 1
fi

# apply_retention rewrites the spans TTL to the configured window. Only the
# spans schema carries a `TTL ... INTERVAL N DAY` line, so this is a no-op for
# every other migration file.
apply_retention() {
  if [ "${RETENTION_DAYS}" -eq 0 ]; then
    grep -v '^TTL '   # keep forever: strip the TTL clause (statement stays valid)
  else
    sed -E "s/(INTERVAL )[0-9]+( DAY)/\1${RETENTION_DAYS}\2/"
  fi
}

ch_query() {
  local sql="$1"
  curl -sS --fail-with-body \
    -u "default:${CLICKHOUSE_PASSWORD}" \
    "${CLICKHOUSE_HTTP}/" \
    --data-binary "${sql}"
}

echo "Waiting for ClickHouse at ${CLICKHOUSE_HTTP}..."
until curl -s -f "${CLICKHOUSE_HTTP}/ping" >/dev/null 2>&1; do
  sleep 2
done
echo "  ClickHouse ready."

echo "Waiting for Postgres..."
until pg_isready -d "${POSTGRES_DSN}" >/dev/null 2>&1; do
  sleep 2
done
echo "  Postgres ready."

echo "Creating ClickHouse database..."
ch_query "CREATE DATABASE IF NOT EXISTS tracium"

echo "Ensuring migration tracking table..."
psql "${POSTGRES_DSN}" -v ON_ERROR_STOP=1 -q -c "
  CREATE TABLE IF NOT EXISTS schema_migrations (
    filename   TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
  )
"

if [ "${RETENTION_DAYS}" -eq 0 ]; then
  echo "Span retention: unlimited (no TTL)"
else
  echo "Span retention: ${RETENTION_DAYS} days"
fi

echo "Running migrations from ${MIGRATIONS_DIR}..."
for sql_file in $(find -L "${MIGRATIONS_DIR}" -maxdepth 1 -type f \( -name "*.sql" -o -name "*.sh" \) | sort); do
  filename=$(basename "${sql_file}")
  count=$(psql "${POSTGRES_DSN}" -v ON_ERROR_STOP=1 -t -c \
    "SELECT COUNT(*) FROM schema_migrations WHERE filename='${filename}'" | xargs)

  if [ "${count}" = "0" ]; then
    echo "  Applying: ${filename}"
    case "$sql_file" in
      *.sql) ch_query "$(apply_retention < "${sql_file}")" ;;
      *.sh) source "$sql_file" ;;
    esac
    psql "${POSTGRES_DSN}" -v ON_ERROR_STOP=1 -q -c \
      "INSERT INTO schema_migrations (filename) VALUES ('${filename}')"
    echo "  Applied:  ${filename}"
  else
    echo "  Skipping (already applied): ${filename}"
  fi
done

echo "Migrations complete."
