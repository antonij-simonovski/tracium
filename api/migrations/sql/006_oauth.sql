-- OAuth for MCP clients. A client registers itself (RFC 7591) and is identified
-- only by its name and redirect URIs; it holds no secret and proves each code
-- exchange with PKCE. Approving a client mints an ordinary read key for the
-- approving user, so revoking that key on the API keys page disconnects it.
CREATE TABLE IF NOT EXISTS oauth_clients (
 id UUID PRIMARY KEY,
 name TEXT NOT NULL,
 redirect_uris TEXT[] NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Authorization codes live for a minute and are deleted when exchanged, so each
-- works once. Only the SHA-256 of the code is stored.
CREATE TABLE IF NOT EXISTS oauth_codes (
 code_hash TEXT PRIMARY KEY,
 client_id UUID NOT NULL REFERENCES oauth_clients (id) ON DELETE CASCADE,
 user_id TEXT NOT NULL,
 redirect_uri TEXT NOT NULL,
 code_challenge TEXT NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS oauth_codes_expires_at_idx ON oauth_codes (expires_at);
