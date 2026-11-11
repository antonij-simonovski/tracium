-- A budget rule is a fixed cost limit per UTC day or month. subject '*' with
-- scope workflow or client applies the limit to each one separately.
CREATE TABLE IF NOT EXISTS alert_rules (
 id TEXT PRIMARY KEY,
 workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
 scope TEXT NOT NULL CHECK (scope IN ('workspace','workflow','client')),
 subject TEXT NOT NULL DEFAULT '',
 period TEXT NOT NULL CHECK (period IN ('day','month')),
 threshold_usd DOUBLE PRECISION NOT NULL CHECK (threshold_usd > 0),
 enabled BOOLEAN NOT NULL DEFAULT true,
 created_by TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS alert_rules_workspace_idx ON alert_rules (workspace_id);
