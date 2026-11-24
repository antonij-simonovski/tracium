-- Reviews of a run (trace) or of one span in it; span_id is '' for the run.
-- workflow_name, workflow_version and output are copied at review time: spans
-- expire with the ClickHouse TTL, and accuracy per version must not need a
-- cross-store join. source is 'api' when an ingest key sent the review.
CREATE TABLE IF NOT EXISTS run_reviews (
 workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
 trace_id TEXT NOT NULL,
 span_id TEXT NOT NULL DEFAULT '',
 span_name TEXT NOT NULL DEFAULT '',
 workflow_name TEXT NOT NULL,
 workflow_version TEXT NOT NULL,
 verdict TEXT NOT NULL CHECK (verdict IN ('correct', 'incorrect', 'missing')),
 output TEXT NOT NULL,
 corrected_output TEXT,
 note TEXT NOT NULL DEFAULT '',
 reviewed_by TEXT NOT NULL,
 source TEXT NOT NULL DEFAULT 'user' CHECK (source IN ('user', 'api')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY (workspace_id, trace_id, span_id)
);
CREATE INDEX IF NOT EXISTS run_reviews_workflow_idx ON run_reviews (workspace_id, workflow_name, span_name, workflow_version);

-- One row per graded JSON field of a review, for per-field accuracy. missing
-- means the output left out a field the correction has.
CREATE TABLE IF NOT EXISTS run_review_fields (
 workspace_id TEXT NOT NULL,
 trace_id TEXT NOT NULL,
 span_id TEXT NOT NULL,
 span_name TEXT NOT NULL,
 workflow_name TEXT NOT NULL,
 workflow_version TEXT NOT NULL,
 field_path TEXT NOT NULL,
 correct BOOLEAN NOT NULL,
 missing BOOLEAN NOT NULL DEFAULT false,
 PRIMARY KEY (workspace_id, trace_id, span_id, field_path),
 FOREIGN KEY (workspace_id, trace_id, span_id) REFERENCES run_reviews (workspace_id, trace_id, span_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS run_review_fields_workflow_idx ON run_review_fields (workspace_id, workflow_name, span_name, workflow_version, field_path);
