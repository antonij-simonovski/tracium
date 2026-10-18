# Sourced by run-migrations.sh. Expires rejected-span counts on the same
# RETENTION_DAYS window as tracium.spans; 0 keeps them forever.
if [ "${RETENTION_DAYS}" -gt 0 ]; then
  ch_query "ALTER TABLE tracium.rejected_spans MODIFY TTL hour + INTERVAL ${RETENTION_DAYS} DAY"
fi
