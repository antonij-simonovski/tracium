import React, { createContext, useContext, useMemo, useState } from 'react';
import { TracesAPI } from '../api/traces';
import { MetricsAPI } from '../api/metrics';
import { WorkspacesAPI } from '../api/workspaces';
import { ApiKeysAPI } from '../api/apikeys';
import { UsersAPI } from '../api/users';
import type { APIClientConfig } from '../interfaces';

interface APIContextValue {
  tracesAPI: TracesAPI;
  metricsAPI: MetricsAPI;
  workspacesAPI: WorkspacesAPI;
  apiKeysAPI: ApiKeysAPI;
  usersAPI: UsersAPI;
  // Fold into every query key so a workspace switch never serves another
  // workspace's cache and refetches with the rebuilt clients.
  workspaceId: string | undefined;
  setWorkspaceId: (id: string | undefined) => void;
}

const APIContext = createContext<APIContextValue | null>(null);

interface APIProviderProps {
  config: APIClientConfig;
  children: React.ReactNode;
}

export function APIProvider({ config, children }: APIProviderProps) {
  const [workspaceId, setWorkspaceId] = useState<string | undefined>(config.workspaceId);

  const { baseUrl, apiKey, timeoutMs } = config;
  const value = useMemo(() => {
    const scopedConfig = { baseUrl, apiKey, timeoutMs, workspaceId };
    return {
      tracesAPI: new TracesAPI(scopedConfig),
      metricsAPI: new MetricsAPI(scopedConfig),
      workspacesAPI: new WorkspacesAPI(scopedConfig),
      apiKeysAPI: new ApiKeysAPI(scopedConfig),
      usersAPI: new UsersAPI(scopedConfig),
      workspaceId,
      setWorkspaceId,
    };
  }, [baseUrl, apiKey, timeoutMs, workspaceId]);

  return <APIContext.Provider value={value}>{children}</APIContext.Provider>;
}

export function useAPIClient(): APIContextValue {
  const ctx = useContext(APIContext);
  if (!ctx) throw new Error('useAPIClient must be used within APIProvider');
  return ctx;
}
