import { useQuery } from '@tanstack/react-query';
import { useAPIClient } from '../../../common/providers/APIProvider';

// One hook per overview section so each loads and refreshes independently.
// All are keyed by the active workspace + range: flipping either refetches, and
// keying on the workspace keeps one workspace's cached result from being shown
// for another (the reads are scoped to workspaceId server-side).

export function useKpis(range: string) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['overview', 'kpis', workspaceId, range],
    queryFn: () => metricsAPI.getKpis(range),
  });
}

export function useCostSeries(range: string) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['overview', 'cost-series', workspaceId, range],
    queryFn: () => metricsAPI.getCostSeries(range),
  });
}

export function useLatencySeries(range: string) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['overview', 'latency-series', workspaceId, range],
    queryFn: () => metricsAPI.getLatencySeries(range),
  });
}

export function useErrorSeries(range: string) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['overview', 'error-series', workspaceId, range],
    queryFn: () => metricsAPI.getErrorSeries(range),
  });
}

export function useTopWorkflows(range: string) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['overview', 'top-workflows', workspaceId, range],
    queryFn: () => metricsAPI.getTopWorkflows(range),
  });
}

export function useFailures(range: string) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['overview', 'failures', workspaceId, range],
    queryFn: () => metricsAPI.getFailures(range),
  });
}

// useAnomalies loads detected outliers for the window. Detection is daily and
// rollup-backed, so the API rejects sub-daily ranges (24h) — the query is
// disabled there rather than firing a request that would 400.
export function useAnomalies(range: string) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['overview', 'anomalies', workspaceId, range],
    queryFn: () => metricsAPI.getAnomalies(range),
    enabled: range !== '24h',
  });
}

// Always the last 24h, whatever range the page shows: checks describe the current setup.
export function useSetupChecks() {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['overview', 'setup-checks', workspaceId],
    queryFn: () => metricsAPI.getSetupChecks('24h'),
  });
}

// useRecentActivity powers the live feed off the most recent traces, polling
// every few seconds.
export function useRecentActivity() {
  const { tracesAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['overview', 'recent-activity', workspaceId],
    queryFn: () => tracesAPI.listTraces({ page_size: 15 }),
    refetchInterval: 5000,
  });
}
