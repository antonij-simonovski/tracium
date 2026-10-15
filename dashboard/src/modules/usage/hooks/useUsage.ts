import { useQuery } from '@tanstack/react-query';
import { useAPIClient } from '../../../common/providers/APIProvider';

// Usage-page breakdowns. The KPI strip and daily chart reuse the overview
// metrics hooks (useKpis / useCostSeries / useErrorSeries); these three cover
// the page's own breakdown endpoints. All keyed by range, so the range selector
// refetches everything.

export function useModelCosts(range: string) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['usage', 'model-costs', workspaceId, range],
    queryFn: () => metricsAPI.getModelCosts(range),
  });
}

export function useTokenSeries(range: string) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['usage', 'token-series', workspaceId, range],
    queryFn: () => metricsAPI.getTokenSeries(range),
  });
}

export function useUserUsage(range: string) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['usage', 'users', workspaceId, range],
    queryFn: () => metricsAPI.getUserUsage(range),
  });
}

export function useWorkflowUsage(range: string) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['usage', 'workflows', workspaceId, range],
    queryFn: () => metricsAPI.getWorkflowUsage(range),
  });
}

// Custom-attribute allocation reads raw spans, so the server rejects long ranges.
export function useAttributeKeys(range: string, enabled: boolean) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['usage', 'attribute-keys', workspaceId, range],
    queryFn: () => metricsAPI.getAttributeKeys(range),
    enabled,
  });
}

export function useAttributeUsage(range: string, key: string) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['usage', 'attribute-usage', workspaceId, range, key],
    queryFn: () => metricsAPI.getUsageByAttribute(range, key),
    enabled: key !== '',
  });
}
