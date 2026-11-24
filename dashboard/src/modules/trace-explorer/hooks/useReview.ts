import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { APIError } from '../../../common/api';
import { useAPIClient } from '../../../common/providers/APIProvider';
import type { ReviewInput } from '../../../common/interfaces';

/** Under the 'reviews' key, so saving a review refreshes it. */
export function useReviewQueue(workflow: string, enabled = true) {
  const { tracesAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['reviews', workspaceId, 'queue', workflow],
    queryFn: () => tracesAPI.getReviewQueue(workflow),
    enabled,
  });
}

export function useReview(traceId: string, spanId?: string, onSaved?: () => void) {
  const { tracesAPI, workspaceId } = useAPIClient();
  const queryClient = useQueryClient();
  const key = ['review', workspaceId, traceId, spanId ?? ''];

  const query = useQuery({
    queryKey: key,
    queryFn: () => tracesAPI.getReview(traceId, spanId).catch(err => {
      if (err instanceof APIError && err.isNotFound) return null;
      throw err;
    }),
  });

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: key });
    queryClient.invalidateQueries({ queryKey: ['workflow-accuracy'] });
    queryClient.invalidateQueries({ queryKey: ['field-accuracy'] });
    queryClient.invalidateQueries({ queryKey: ['reviews'] });
  };
  const save = useMutation({ mutationFn: (input: ReviewInput) => tracesAPI.saveReview(traceId, input, spanId), onSuccess: () => { refresh(); onSaved?.(); } });
  const remove = useMutation({ mutationFn: () => tracesAPI.deleteReview(traceId, spanId), onSuccess: refresh });

  return { review: query.data ?? null, isLoading: query.isLoading, error: query.error, save, remove };
}
