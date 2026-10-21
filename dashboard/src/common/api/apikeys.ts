import { BaseAPIClient } from './client';

// Ingest keys belong to a workspace and send its telemetry. Read keys belong to
// the signed-in account and query, through the query API and MCP, every
// workspace it can read.
export type ApiKeyScope = 'ingest' | 'read';

export interface ApiKeyRecord {
  id: string;
  /** Absent on read keys, which belong to an account rather than a workspace. */
  workspace_id?: string;
  created_by: string;
  name: string;
  prefix: string;
  scope: ApiKeyScope;
  created_at: string;
  last_used_at: string | null;
  revoked_at: string | null;
}

// The only response that carries the plaintext token.
export interface CreatedApiKey {
  key: ApiKeyRecord;
  token: string;
}

export class ApiKeysAPI extends BaseAPIClient {
  list(workspaceId: string): Promise<ApiKeyRecord[]> {
    return this.get<ApiKeyRecord[]>(`/workspaces/${encodeURIComponent(workspaceId)}/api-keys`);
  }

  create(workspaceId: string, name: string): Promise<CreatedApiKey> {
    return this.post<CreatedApiKey>(`/workspaces/${encodeURIComponent(workspaceId)}/api-keys`, { name });
  }

  revoke(workspaceId: string, keyId: string): Promise<void> {
    return this.delete(`/workspaces/${encodeURIComponent(workspaceId)}/api-keys/${encodeURIComponent(keyId)}`);
  }

  listRead(): Promise<ApiKeyRecord[]> {
    return this.get<ApiKeyRecord[]>('/account/api-keys');
  }

  createRead(name: string): Promise<CreatedApiKey> {
    return this.post<CreatedApiKey>('/account/api-keys', { name });
  }

  revokeRead(keyId: string): Promise<void> {
    return this.delete(`/account/api-keys/${encodeURIComponent(keyId)}`);
  }
}
