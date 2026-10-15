import { fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { VersionCompareLivePage } from './VersionCompareLivePage';

const api = vi.hoisted(() => ({
  getCostSeries: vi.fn(),
  getLatencySeries: vi.fn(),
  getErrorSeries: vi.fn(),
  getTokenSeries: vi.fn(),
  getWorkflowVersions: vi.fn(),
}));
vi.mock('../../../common/providers/APIProvider', () => ({
  useAPIClient: () => ({ metricsAPI: api }),
}));

const empty = { items: [], total: 0, page: 1, page_size: 20 };
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
  api.getCostSeries.mockResolvedValue(empty);
  api.getLatencySeries.mockResolvedValue(empty);
  api.getErrorSeries.mockResolvedValue(empty);
  api.getTokenSeries.mockResolvedValue(empty);
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
    expect(screen.getByRole('button', { name: /p95 latency/ })).toHaveTextContent('0%');
  });

  it('charts tokens per run for each version', async () => {
    show();
    fireEvent.click(await screen.findByRole('button', { name: /Tokens \/ run/ }));
    expect(api.getTokenSeries).toHaveBeenCalledWith('7d', undefined, 'checkout', 'v2');
    expect(api.getTokenSeries).toHaveBeenCalledWith('7d', undefined, 'checkout', 'v3');
  });

  it('charts up to two metrics, dropping the oldest pick', async () => {
    show();
    const cpr = await screen.findByRole('button', { name: /Cost \/ run/ });
    const p95 = screen.getByRole('button', { name: /p95 latency/ });
    const err = screen.getByRole('button', { name: /Error rate/ });
    fireEvent.click(p95);
    expect(cpr).toHaveAttribute('aria-pressed', 'true');
    expect(p95).toHaveAttribute('aria-pressed', 'true');
    expect(api.getLatencySeries).toHaveBeenCalledWith('7d', 'checkout', 'v3');
    fireEvent.click(err);
    expect(cpr).toHaveAttribute('aria-pressed', 'false');
    expect(err).toHaveAttribute('aria-pressed', 'true');
    fireEvent.click(err);
    fireEvent.click(p95);
    expect(p95).toHaveAttribute('aria-pressed', 'true');
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
    api.getWorkflowVersions.mockResolvedValue({ ...empty, items: [version('v2', 0.004, 0.002)] });
    show();
    expect(await screen.findByText('Nothing to compare in this range')).toBeInTheDocument();
  });
});
