import { useQuery, type Query } from '@tanstack/react-query';
import { useAPIClient } from '../../../common/providers/APIProvider';

// Same 1-minute refresh grid as the workflows list.
const DETAIL_REFRESH_MS = 60_000;

const WORKFLOW_RUNS_PAGE_SIZE = 10;

// Keys are [scope, kind, workspaceId, name, range, version?]: a range or version
// change keeps the page up, but another workflow or workspace never shows these
// numbers.
export const shared = (workspaceId: string | undefined, name: string, enabled: boolean) => ({
  staleTime: DETAIL_REFRESH_MS,
  refetchInterval: DETAIL_REFRESH_MS,
  refetchIntervalInBackground: false,
  placeholderData: <T,>(previous: T | undefined, query: Query<T, Error, T, readonly unknown[]> | undefined) =>
    query?.queryKey[2] === workspaceId && query?.queryKey[3] === name ? previous : undefined,
  enabled,
});

export function useWorkflowDetail(name: string, range: string, enabled = true) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['workflows', 'detail', workspaceId, name, range],
    queryFn: () => metricsAPI.getWorkflowDetail(name, range),
    ...shared(workspaceId, name, enabled),
  });
}

export function useWorkflowCostSeries(name: string, range: string, enabled = true, version?: string) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['workflows', 'cost-series', workspaceId, name, range, version],
    queryFn: () => metricsAPI.getCostSeries(range, name, undefined, version),
    ...shared(workspaceId, name, enabled),
  });
}

export function useWorkflowLatencySeries(name: string, range: string, enabled = true, version?: string) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['workflows', 'latency-series', workspaceId, name, range, version],
    queryFn: () => metricsAPI.getLatencySeries(range, name, version),
    ...shared(workspaceId, name, enabled),
  });
}

export function useWorkflowErrorSeries(name: string, range: string, enabled = true, version?: string) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['workflows', 'error-series', workspaceId, name, range, version],
    queryFn: () => metricsAPI.getErrorSeries(range, name, undefined, version),
    ...shared(workspaceId, name, enabled),
  });
}

export function useWorkflowRuns(name: string, range: string, enabled = true, version?: string) {
  const { tracesAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['workflows', 'runs', workspaceId, name, range, version],
    queryFn: () => tracesAPI.listTraces({ workflow: name, version, range, page_size: WORKFLOW_RUNS_PAGE_SIZE }),
    ...shared(workspaceId, name, enabled),
  });
}
