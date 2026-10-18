import { BaseAPIClient } from './client';

export interface ApiKeyRecord {
  id: string;
  workspace_id: string;
  created_by: string;
  name: string;
  prefix: string;
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
}
