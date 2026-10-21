-- 'ingest' keys belong to a workspace and send its telemetry. 'read' keys belong
-- to the user who created them and query, through the query API and MCP, the
-- workspaces that user is a member of at the time of each request, so leaving a
-- workspace also ends the key's access to it.
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS scope TEXT NOT NULL DEFAULT 'ingest';
ALTER TABLE api_keys ALTER COLUMN workspace_id DROP NOT NULL;
ALTER TABLE api_keys DROP CONSTRAINT IF EXISTS api_keys_scope_owner;
ALTER TABLE api_keys ADD CONSTRAINT api_keys_scope_owner CHECK (
  (scope = 'ingest' AND workspace_id IS NOT NULL) OR (scope = 'read' AND workspace_id IS NULL));
CREATE INDEX IF NOT EXISTS api_keys_read_created_by_idx ON api_keys (created_by) WHERE scope = 'read';
