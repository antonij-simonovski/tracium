import { fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { APIError } from '../../../common/api/client';
import { VersionCompareLivePage } from './VersionCompareLivePage';

const api = vi.hoisted(() => ({
  compareWorkflowVersions: vi.fn(),
  getCostSeries: vi.fn(),
  getLatencySeries: vi.fn(),
  getErrorSeries: vi.fn(),
  getWorkflowVersions: vi.fn(),
}));
vi.mock('../../../common/providers/APIProvider', () => ({
  useAPIClient: () => ({ metricsAPI: api }),
}));

const empty = { items: [], total: 0, page: 1, page_size: 20 };
const kpi = (delta: number, delta_type = 'neutral') => ({ value: 0, delta, delta_type });
const version = (v: string, cpr: number, err: number) => ({
  version: v, first_seen_ms: 0, last_seen_ms: 1000, calls: 100, cost: 100 * cpr,
  cost_per_run: cpr, p95_latency_ms: 2500, error_rate: err, tokens_per_run: 1500,
});

function show(range = '7d', onSelect = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <VersionCompareLivePage workflowName="checkout" base="v2" candidate="v3" range={range} onSelect={onSelect} />
    </QueryClientProvider>,
  );
  return onSelect;
}

beforeEach(() => {
  vi.resetAllMocks();
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} unobserve() {} });
  api.compareWorkflowVersions.mockResolvedValue({
    base: version('v2', 0.004, 0.002),
    candidate: version('v3', 0.006, 0.006),
    cost_per_run: kpi(0.5, 'bad'),
    p95_latency_ms: kpi(0),
    error_rate: kpi(2),
    tokens_per_run: kpi(0),
  });
  api.getCostSeries.mockResolvedValue(empty);
  api.getLatencySeries.mockResolvedValue(empty);
  api.getErrorSeries.mockResolvedValue(empty);
  api.getWorkflowVersions.mockResolvedValue({
    ...empty,
    items: [version('v1', 0.003, 0.001), version('v2', 0.004, 0.002), version('v3', 0.006, 0.006)],
  });
});

afterEach(() => vi.unstubAllGlobals());

describe('version compare', () => {
  it('shows each metric side by side with its change', async () => {
    show();
    expect(await screen.findByRole('button', { name: /Cost \/ run/ })).toHaveTextContent('+50%');
    expect(screen.getByRole('button', { name: /Error rate/ })).toHaveTextContent('+0.4 pts');
    expect(screen.getByRole('button', { name: /p95 latency/ })).toHaveTextContent('—');
    expect(api.compareWorkflowVersions).toHaveBeenCalledWith('checkout', 'v2', 'v3', '7d');
  });

  it('swaps base and candidate', async () => {
    const onSelect = show();
    fireEvent.click(await screen.findByRole('button', { name: 'Swap' }));
    expect(onSelect).toHaveBeenCalledWith('v3', 'v2');
  });

  it('picks a different base from the versions in range', async () => {
    const onSelect = show();
    fireEvent.click(await screen.findByRole('button', { name: 'Base: v2' }));
    expect(screen.queryByRole('option', { name: /v3/ })).not.toBeInTheDocument();
    fireEvent.click(await screen.findByRole('option', { name: /v1/ }));
    expect(onSelect).toHaveBeenCalledWith('v1', 'v3');
  });

  it('picks a different candidate', async () => {
    const onSelect = show();
    fireEvent.click(await screen.findByRole('button', { name: 'Candidate: v3' }));
    fireEvent.click(await screen.findByRole('option', { name: /v1/ }));
    expect(onSelect).toHaveBeenCalledWith('v2', 'v1');
  });

  it('skips the version series over rollup ranges', async () => {
    show('90d');
    expect(await screen.findByText('Over-time charts need a range of 30 days or less.')).toBeInTheDocument();
    expect(api.getErrorSeries).not.toHaveBeenCalled();
    expect(api.getCostSeries).not.toHaveBeenCalled();
  });

  it('explains when a version has no runs in the range', async () => {
    api.compareWorkflowVersions.mockRejectedValue(new APIError('not found', 404, 'not_found'));
    show();
    expect(await screen.findByText('Nothing to compare in this range')).toBeInTheDocument();
  });
});
