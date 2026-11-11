-- A channel can also hear when a new version's first grade is healthy. Those
-- notices are delivered like incident events but belong to no incident, so the
-- incident foreign key goes; deliveries still cascade with their channel.
ALTER TABLE alert_channels ADD COLUMN IF NOT EXISTS notify_healthy_releases BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE alert_deliveries DROP CONSTRAINT IF EXISTS alert_deliveries_incident_id_fkey;
