import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useAPIClient } from '../../../common/providers/APIProvider';
import type { IncidentStatus, MuteInput } from '../interfaces';

const REFRESH_MS = 60_000;

export function useIncidents(status: IncidentStatus, page = 1) {
  const { incidentsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['incidents', 'list', workspaceId, status, page],
    queryFn: () => incidentsAPI.listIncidents(status, page),
    staleTime: REFRESH_MS,
    refetchInterval: REFRESH_MS,
    refetchIntervalInBackground: false,
  });
}

// One cached call shared by the sidebar badge, the overview strip and the workflow status dots.
export function useOpenIncidents(enabled = true) {
  const { incidentsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['incidents', 'open', workspaceId],
    queryFn: () => incidentsAPI.listIncidents('open', 1, 100),
    enabled: enabled && !!workspaceId,
    staleTime: REFRESH_MS,
    refetchInterval: REFRESH_MS,
    refetchIntervalInBackground: false,
  });
}

export function useIncident(id: string) {
  const { incidentsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['incidents', 'detail', workspaceId, id],
    queryFn: () => incidentsAPI.getIncident(id),
  });
}

export function useIncidentActions(id: string) {
  const { incidentsAPI } = useAPIClient();
  const queryClient = useQueryClient();
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['incidents'] });
  const resolve = useMutation({ mutationFn: () => incidentsAPI.resolveIncident(id), onSuccess: refresh });
  const mute = useMutation({ mutationFn: (input: MuteInput) => incidentsAPI.muteIncident(id, input), onSuccess: refresh });
  return { resolve, mute };
}
