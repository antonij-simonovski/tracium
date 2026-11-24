CREATE MATERIALIZED VIEW IF NOT EXISTS tracium.service_activity_hourly_mv
TO tracium.activity_hourly AS
SELECT
    workspace_id,
    'service'    AS dim,
    service_name AS key,
    toStartOfHour(toDateTime(intDiv(start_time_ms, 1000))) AS hour,
    count()      AS spans
FROM tracium.spans
WHERE source = 'span' AND service_name != ''
GROUP BY workspace_id, dim, key, hour;
