# Sourced by run-migrations.sh. Expires activity on the RETENTION_DAYS window of
# tracium.spans (0 keeps it), and backfills the 35 days before the view started
# filling it so client heartbeats have a weekly profile on upgrade.
if [ "${RETENTION_DAYS}" -gt 0 ]; then
  ch_query "ALTER TABLE tracium.activity_hourly MODIFY TTL hour + INTERVAL ${RETENTION_DAYS} DAY"
fi

ch_query "INSERT INTO tracium.activity_hourly
  SELECT workspace_id, 'client', user_id, toStartOfHour(toDateTime(intDiv(start_time_ms, 1000))) AS hour, count()
  FROM tracium.spans
  WHERE source = 'span' AND user_id != ''
    AND start_time_ms >= toUnixTimestamp(now() - INTERVAL 35 DAY) * 1000
    AND hour < (SELECT if(count() = 0, toStartOfHour(now()) + 3600, min(hour)) FROM tracium.activity_hourly WHERE dim = 'client')
  GROUP BY workspace_id, user_id, hour"
