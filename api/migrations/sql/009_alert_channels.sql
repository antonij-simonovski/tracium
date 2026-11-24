-- target is a Slack or webhook URL (a credential, never returned in full) or
-- comma-separated email addresses. A delivery is unique per incident state
-- change (event_at), so a repeated notify enqueues nothing and a reopen gets
-- the next seq.
CREATE TABLE IF NOT EXISTS alert_channels (
 id TEXT PRIMARY KEY,
 workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK (kind IN ('slack','webhook','email')),
 name TEXT NOT NULL,
 target TEXT NOT NULL,
 secret TEXT NOT NULL DEFAULT '',
 min_severity TEXT NOT NULL DEFAULT 'warning' CHECK (min_severity IN ('warning','critical')),
 notify_resolved BOOLEAN NOT NULL DEFAULT true,
 enabled BOOLEAN NOT NULL DEFAULT true,
 created_by TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS alert_channels_workspace_idx ON alert_channels (workspace_id);

CREATE TABLE IF NOT EXISTS alert_deliveries (
 incident_id TEXT NOT NULL REFERENCES incidents (id) ON DELETE CASCADE,
 channel_id TEXT NOT NULL REFERENCES alert_channels (id) ON DELETE CASCADE,
 event TEXT NOT NULL,
 seq INT NOT NULL DEFAULT 0,
 event_at TIMESTAMPTZ NOT NULL,
 status TEXT NOT NULL CHECK (status IN ('pending','sent','failed')),
 attempts INT NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '',
 payload JSONB NOT NULL,
 next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 sent_at TIMESTAMPTZ,
 PRIMARY KEY (incident_id, channel_id, event, seq)
);
CREATE UNIQUE INDEX IF NOT EXISTS alert_deliveries_event_idx ON alert_deliveries (incident_id, channel_id, event, event_at);
CREATE INDEX IF NOT EXISTS alert_deliveries_pending_idx ON alert_deliveries (next_attempt_at) WHERE status = 'pending';
