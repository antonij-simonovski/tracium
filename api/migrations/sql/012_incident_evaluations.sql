-- When the incident evaluator last finished a workspace; only the replica
-- holding the advisory lock evaluates, so the time must live here.
CREATE TABLE IF NOT EXISTS incident_evaluations (
 workspace_id TEXT PRIMARY KEY REFERENCES workspaces (id) ON DELETE CASCADE,
 evaluated_at TIMESTAMPTZ NOT NULL
);
