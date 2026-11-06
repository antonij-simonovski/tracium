import { useQuery } from '@tanstack/react-query';
import { useAPIClient } from '../../../common/providers/APIProvider';

/** span is a span name; '' means run reviews. */
export function useWorkflowAccuracy(name: string, span = '') {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['workflow-accuracy', workspaceId, name, span],
    queryFn: () => metricsAPI.getWorkflowAccuracy(name, span),
  });
}

export function useFieldAccuracy(name: string, version: string, span = '', enabled = true) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['field-accuracy', workspaceId, name, version, span],
    queryFn: () => metricsAPI.getFieldAccuracy(name, version, span),
    enabled,
  });
}

/** Span names with reviews, most reviewed first; version omitted means every version. */
export function useReviewedSpans(name: string, version?: string) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['reviews', workspaceId, name, version ?? null, 'spans'],
    queryFn: () => metricsAPI.getReviewedSpans(name, version),
  });
}

export function useCorrections(name: string, version: string, span: string, field: string | null, page: number, enabled = true) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['reviews', workspaceId, name, version, span, field, page],
    queryFn: () => metricsAPI.getReviews(name, { version, verdicts: ['incorrect', 'missing'], span: span || undefined, field: field ?? undefined, page }),
    placeholderData: previous => previous,
    enabled,
  });
}

/** Accuracy per version of the whole run (span '') and of every reviewed span, in one call. */
export function useStepAccuracy(name: string) {
  const { metricsAPI, workspaceId } = useAPIClient();
  return useQuery({
    queryKey: ['workflow-accuracy', workspaceId, name, '*'],
    queryFn: () => metricsAPI.getStepAccuracy(name),
  });
}
