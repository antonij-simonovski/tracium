import { fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ReviewQueue } from './ReviewQueue';

const api = vi.hoisted(() => ({ getReviewQueue: vi.fn() }));
vi.mock('../../../common/providers/APIProvider', () => ({
  useAPIClient: () => ({ tracesAPI: api, workspaceId: 'ws' }),
}));

function show(onOpen = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><ReviewQueue workflow="wf" onOpen={onOpen} /></QueryClientProvider>);
  return onOpen;
}

beforeEach(() => vi.resetAllMocks());

describe('review queue', () => {
  it('lists runs with their reason and opens one', async () => {
    const item = { trace_id: 'run-1', name: 'wf', workflow_version: 'v2', start_time_ms: Date.now(), reason: 'incident', incident_id: 'inc' };
    api.getReviewQueue.mockResolvedValue([item, { ...item, trace_id: 'run-2', reason: 'sample', incident_id: undefined }]);
    const onOpen = show();
    expect(await screen.findByText('Open incident')).toBeTruthy();
    expect(screen.getByText('Random sample')).toBeTruthy();
    expect(api.getReviewQueue).toHaveBeenCalledWith('wf');
    fireEvent.click(screen.getByText('run-1'));
    expect(onOpen).toHaveBeenCalledWith(item);
  });

  it('says when there is nothing to review', async () => {
    api.getReviewQueue.mockResolvedValue([]);
    show();
    expect(await screen.findByText(/Nothing to review/)).toBeTruthy();
  });
});
