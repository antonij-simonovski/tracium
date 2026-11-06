# Sourced by run-migrations.sh. Backfills the latency rollup for the days before
# its view started filling it, so the detector has a baseline on upgrade.

ch_query "INSERT INTO tracium.metrics_daily_latency
  SELECT toDate(start_time_ms / 1000) AS bucket_date, user_id, workspace_id, workflow_name, workflow_version,
         count(), quantileTDigestState(0.95)(duration_ms)
  FROM tracium.spans
  WHERE source = 'span' AND parent_span_id = ''
    AND toDate(start_time_ms / 1000) < (SELECT if(count() = 0, today() + 1, min(bucket_date)) FROM tracium.metrics_daily_latency)
  GROUP BY bucket_date, user_id, workspace_id, workflow_name, workflow_version"
