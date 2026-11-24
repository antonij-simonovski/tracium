CREATE MATERIALIZED VIEW IF NOT EXISTS tracium.activity_hourly_mv
TO tracium.activity_hourly AS
SELECT
    workspace_id,
    'client' AS dim,
    user_id  AS key,
    toStartOfHour(toDateTime(intDiv(start_time_ms, 1000))) AS hour,
    count()  AS spans
FROM tracium.spans
WHERE source = 'span' AND user_id != ''
GROUP BY workspace_id, dim, key, hour;
