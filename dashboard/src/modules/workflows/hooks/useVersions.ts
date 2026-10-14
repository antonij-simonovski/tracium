import { useQuery } from '@tanstack/react-query';
import { useAPIClient } from '../../../common/providers/APIProvider';
import { shared } from './useWorkflowDetail';

export function useWorkflowVersions(name: string, range: string, enabled = true) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['workflows', 'versions', workspaceId, name, range],
    queryFn: () => metricsAPI.getWorkflowVersions(name, range),
    ...shared(workspaceId, name, enabled),
  });
}

export function useVersionComparison(name: string, base: string, candidate: string, range: string) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['workflows', 'compare', workspaceId, name, range, base, candidate],
    queryFn: () => metricsAPI.compareWorkflowVersions(name, base, candidate, range),
    ...shared(workspaceId, name, true),
  });
}
