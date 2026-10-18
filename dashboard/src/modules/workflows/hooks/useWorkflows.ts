import { useQuery } from '@tanstack/react-query';
import { useAPIClient } from '../../../common/providers/APIProvider';

// Matches the server's 1-minute query-cache grid, so concurrent pollers share
// one scan per minute.
const WORKFLOWS_REFRESH_MS = 60_000;

export function useWorkflows(range: string) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['workflows', 'list', workspaceId, range],
    queryFn: () => metricsAPI.getWorkflows(range),
    staleTime: WORKFLOWS_REFRESH_MS,
    refetchInterval: WORKFLOWS_REFRESH_MS,
    refetchIntervalInBackground: false,
  });
}
