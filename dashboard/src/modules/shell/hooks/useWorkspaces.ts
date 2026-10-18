import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useAPIClient } from '../../../common/providers/APIProvider';
import type { Workspace } from '../interfaces';
import type { CreateWorkspaceInput } from '../../../common/api/workspaces';

const QUERY_KEY = ['workspaces'] as const;

export interface UseWorkspacesResult {
  workspaces: Workspace[];
  isLoading: boolean;
  isError: boolean;
  createWorkspace: (input: CreateWorkspaceInput) => Promise<Workspace>;
  deleteWorkspace: (id: string) => Promise<void>;
}

export function useWorkspaces(enabled = true): UseWorkspacesResult {
  const { workspacesAPI } = useAPIClient();
  const queryClient = useQueryClient();

  const { data: workspaces = [], isLoading, isError } = useQuery({
    queryKey: QUERY_KEY,
    queryFn: () => workspacesAPI.list(),
    staleTime: 60_000,
    enabled,
  });

  const createMutation = useMutation({
    mutationFn: (input: CreateWorkspaceInput) => workspacesAPI.create(input),
    onSuccess: (created) => {
      queryClient.setQueryData<Workspace[]>(QUERY_KEY, (prev = []) => [...prev, created]);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => workspacesAPI.remove(id),
    onSuccess: (_data, id) => {
      queryClient.setQueryData<Workspace[]>(QUERY_KEY, (prev = []) =>
        prev.filter((w) => w.id !== id),
      );
    },
  });

  return {
    workspaces,
    isLoading,
    isError,
    createWorkspace: createMutation.mutateAsync,
    deleteWorkspace: deleteMutation.mutateAsync,
  };
}
