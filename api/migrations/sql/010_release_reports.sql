-- One report per new workflow version. rechecked marks the single re-evaluation
-- an ok verdict gets; a regressed verdict is never flipped back.
CREATE TABLE IF NOT EXISTS release_reports (
 workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
 workflow TEXT NOT NULL,
 version TEXT NOT NULL,
 prev_version TEXT NOT NULL,
 first_seen_at TIMESTAMPTZ NOT NULL,
 status TEXT NOT NULL CHECK (status IN ('pending','final')),
 verdict TEXT NOT NULL DEFAULT '' CHECK (verdict IN ('','ok','improved','regressed','insufficient')),
 runs INT NOT NULL DEFAULT 0,
 prev_runs INT NOT NULL DEFAULT 0,
 findings JSONB NOT NULL DEFAULT '[]',
 rechecked BOOLEAN NOT NULL DEFAULT false,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 finalized_at TIMESTAMPTZ,
 PRIMARY KEY (workspace_id, workflow, version)
);
CREATE INDEX IF NOT EXISTS release_reports_pending_idx ON release_reports (status) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS release_reports_recent_idx ON release_reports (workspace_id, created_at DESC);
