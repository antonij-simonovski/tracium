import { useCallback, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useAPIClient } from '../../../common/providers/APIProvider';
import type { ApiKeyRecord, CreatedApiKey } from '../../../common/api';

// Live ingest-key management, scoped to the active workspace. The list is keyed
// by workspaceId so switching workspaces refetches; mutations refresh it on
// success rather than mutating the cache by hand, so the server stays the source
// of truth for derived fields (prefix, created_at, created_by).

const keysKey = (workspaceId: string | undefined) => ['api-keys', workspaceId] as const;

export interface UseApiKeysResult {
  keys: ApiKeyRecord[];
  isLoading: boolean;
  isError: boolean;
  createKey: (name: string) => Promise<CreatedApiKey>;
  isCreating: boolean;
  revokeKey: (keyId: string) => Promise<void>;
  isRevoking: boolean;
}

export function useApiKeys(workspaceId: string | undefined): UseApiKeysResult {
  const { apiKeysAPI } = useAPIClient();
  // Guarded by `enabled`, so workspaceId is defined whenever these run.
  const ws = workspaceId as string;
  return useKeys(keysKey(workspaceId), !!workspaceId, {
    list: () => apiKeysAPI.list(ws),
    create: (name) => apiKeysAPI.create(ws, name),
    revoke: (keyId) => apiKeysAPI.revoke(ws, keyId),
  });
}

// useReadKeys manages the signed-in account's read keys, which query every
// workspace the account can read through the query API and MCP.
export function useReadKeys(): UseApiKeysResult {
  const { apiKeysAPI } = useAPIClient();
  return useKeys(['read-keys'], true, {
    list: () => apiKeysAPI.listRead(),
    create: (name) => apiKeysAPI.createRead(name),
    revoke: (keyId) => apiKeysAPI.revokeRead(keyId),
  });
}

interface KeyCalls {
  list: () => Promise<ApiKeyRecord[]>;
  create: (name: string) => Promise<CreatedApiKey>;
  revoke: (keyId: string) => Promise<void>;
}

function useKeys(queryKey: readonly unknown[], enabled: boolean, calls: KeyCalls): UseApiKeysResult {
  const queryClient = useQueryClient();
  const refresh = () => void queryClient.invalidateQueries({ queryKey });

  const { data: keys = [], isLoading, isError } = useQuery({ queryKey, queryFn: calls.list, enabled });
  const createMutation = useMutation({ mutationFn: calls.create, onSuccess: refresh });
  const revokeMutation = useMutation({ mutationFn: calls.revoke, onSuccess: refresh });

  return {
    keys,
    isLoading,
    isError,
    createKey: createMutation.mutateAsync,
    isCreating: createMutation.isPending,
    revokeKey: revokeMutation.mutateAsync,
    isRevoking: revokeMutation.isPending,
  };
}

// seedDemoKeys returns a small, believable set of records for the auth-page
// preview — recent timestamps so the table reads as live. The tokens were never
// real; only non-secret metadata is shown, matching the live page's shape.
function seedDemoKeys(): ApiKeyRecord[] {
  const now = Date.now();
  const iso = (msAgo: number) => new Date(now - msAgo).toISOString();
  const day = 24 * 60 * 60 * 1000;
  return [
    {
      id: 'demo-prod',
      workspace_id: 'demo',
      created_by: 'you@example.com',
      name: 'Production ingest',
      prefix: 'trc_9f3a1b2c',
      scope: 'ingest',
      created_at: iso(9 * day),
      last_used_at: iso(4 * 60 * 1000),
      revoked_at: null,
    },
    {
      id: 'demo-staging',
      workspace_id: 'demo',
      created_by: 'you@example.com',
      name: 'Staging ingest',
      prefix: 'trc_4d2e77a0',
      scope: 'ingest',
      created_at: iso(5 * day),
      last_used_at: iso(2 * 60 * 60 * 1000),
      revoked_at: null,
    },
    {
      id: 'demo-old-laptop',
      workspace_id: 'demo',
      created_by: 'you@example.com',
      name: 'Old laptop',
      prefix: 'trc_1a0b9c8d',
      scope: 'ingest',
      created_at: iso(40 * day),
      last_used_at: iso(21 * day),
      revoked_at: iso(20 * day),
    },
  ];
}

// useDemoApiKeys is a self-contained, in-memory stand-in for useApiKeys with the
// same shape, so the auth-page preview drives the real UI (create → one-time
// reveal → revoke) without a backend or network. Nothing here is a real
// credential; the generated token is random display text shown only once.
export function useDemoApiKeys(): UseApiKeysResult {
  const [keys, setKeys] = useState<ApiKeyRecord[]>(seedDemoKeys);

  const createKey = useCallback(async (name: string): Promise<CreatedApiKey> => {
    const rand = () => Math.random().toString(36).slice(2);
    const token = `trc_${(rand() + rand()).slice(0, 32)}`;
    const key: ApiKeyRecord = {
      id: `demo-${rand().slice(0, 8)}`,
      workspace_id: 'demo',
      created_by: 'you@example.com',
      name: name.trim() || 'default',
      prefix: token.slice(0, 12),
      scope: 'ingest',
      created_at: new Date().toISOString(),
      last_used_at: null,
      revoked_at: null,
    };
    setKeys((prev) => [key, ...prev]);
    return { key, token };
  }, []);

  const revokeKey = useCallback(async (keyId: string): Promise<void> => {
    setKeys((prev) =>
      prev.map((k) =>
        k.id === keyId ? { ...k, revoked_at: new Date().toISOString() } : k,
      ),
    );
  }, []);

  return useMemo(
    () => ({
      keys,
      isLoading: false,
      isError: false,
      createKey,
      isCreating: false,
      revokeKey,
      isRevoking: false,
    }),
    [keys, createKey, revokeKey],
  );
}
