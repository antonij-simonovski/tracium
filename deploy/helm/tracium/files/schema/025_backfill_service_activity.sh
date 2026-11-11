# Sourced by run-migrations.sh. Backfills service activity for the 35 days before
# the view started filling it, so ingest silence has a weekly profile on upgrade.
ch_query "INSERT INTO tracium.activity_hourly
  SELECT workspace_id, 'service', service_name, toStartOfHour(toDateTime(intDiv(start_time_ms, 1000))) AS hour, count()
  FROM tracium.spans
  WHERE source = 'span' AND service_name != ''
    AND start_time_ms >= toUnixTimestamp(now() - INTERVAL 35 DAY) * 1000
    AND hour < (SELECT if(count() = 0, toStartOfHour(now()) + 3600, min(hour)) FROM tracium.activity_hourly WHERE dim = 'service')
  GROUP BY workspace_id, service_name, hour"
