-- One open incident per subject; the partial unique index keeps that true
-- under concurrent evaluators. A mute with an empty metric mutes every metric
-- of the subject, and a NULL until mutes it forever.
CREATE TABLE IF NOT EXISTS incidents (
 id TEXT PRIMARY KEY,
 workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
 scope TEXT NOT NULL CHECK (scope IN ('workspace','workflow','client','service')),
 subject TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL CHECK (status IN ('open','resolved')),
 severity TEXT NOT NULL CHECK (severity IN ('info','warning','critical')),
 title TEXT NOT NULL,
 version_change JSONB,
 opened_at TIMESTAMPTZ NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL,
 last_signal_at TIMESTAMPTZ NOT NULL,
 resolved_at TIMESTAMPTZ,
 resolved_by TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS incidents_open_subject_idx
 ON incidents (workspace_id, scope, subject) WHERE status = 'open';
CREATE INDEX IF NOT EXISTS incidents_workspace_idx ON incidents (workspace_id, status, opened_at DESC);

CREATE TABLE IF NOT EXISTS incident_signals (
 incident_id TEXT NOT NULL REFERENCES incidents (id) ON DELETE CASCADE,
 metric TEXT NOT NULL,
 bucket_ms BIGINT NOT NULL,
 observed DOUBLE PRECISION NOT NULL,
 expected DOUBLE PRECISION NOT NULL,
 score DOUBLE PRECISION NOT NULL,
 direction TEXT NOT NULL,
 severity TEXT NOT NULL,
 summary TEXT NOT NULL,
 trace_ids TEXT[] NOT NULL DEFAULT '{}',
 PRIMARY KEY (incident_id, metric, bucket_ms)
);

CREATE TABLE IF NOT EXISTS incident_mutes (
 id TEXT PRIMARY KEY,
 workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
 scope TEXT NOT NULL,
 subject TEXT NOT NULL DEFAULT '',
 metric TEXT NOT NULL DEFAULT '',
 until TIMESTAMPTZ,
 created_by TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS incident_mutes_workspace_idx ON incident_mutes (workspace_id);
