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
  getWorkflowAccuracy: vi.fn(),
  getFieldAccuracy: vi.fn(),
  getReviewedSpans: vi.fn(),
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
  api.getWorkflowAccuracy.mockResolvedValue([]);
  api.getFieldAccuracy.mockResolvedValue([]);
  api.getReviewedSpans.mockResolvedValue([]);
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

  it('compares over rollup ranges without charting latency', async () => {
    show('90d');
    expect(await screen.findByRole('button', { name: /p95 latency/ })).toBeDisabled();
    expect(api.getErrorSeries).toHaveBeenCalledWith('90d', 'checkout', undefined, 'v3');
    expect(api.getCostSeries).toHaveBeenCalledWith('90d', 'checkout', undefined, 'v2');
    expect(api.getLatencySeries).not.toHaveBeenCalled();
  });

  it('compares reviewed accuracy overall and per field', async () => {
    api.getWorkflowAccuracy.mockResolvedValue([
      { version: 'v2', reviewed_runs: 10, correct_runs: 8, accuracy: 0.8 },
      { version: 'v3', reviewed_runs: 10, correct_runs: 6, accuracy: 0.6 },
    ]);
    api.getFieldAccuracy.mockImplementation(async (_: string, v: string) =>
      v === 'v2' ? [{ path: 'total', reviewed: 10, wrong: 1, accuracy: 0.9 }] : [{ path: 'total', reviewed: 10, wrong: 4, accuracy: 0.6 }]);
    show();
    const row = await screen.findByRole('button', { name: /Accuracy/ });
    expect(row).toHaveTextContent('80%');
    expect(row).toHaveTextContent('60%');
    expect(row).toHaveTextContent('-20.0 pts');
    expect(row).toBeDisabled();
    expect(await screen.findByRole('row', { name: /total/ })).toHaveTextContent('-30.0 pts');
  });

  it('compares accuracy of a reviewed span', async () => {
    api.getWorkflowAccuracy.mockImplementation(async (_: string, span: string) =>
      span === 'parse' ? [{ version: 'v2', reviewed_runs: 4, correct_runs: 2, accuracy: 0.5 }, { version: 'v3', reviewed_runs: 4, correct_runs: 4, accuracy: 1 }]
        : [{ version: 'v3', reviewed_runs: 2, correct_runs: 1, accuracy: 0.5 }]);
    api.getReviewedSpans.mockImplementation(async (_: string, v: string) => (v === 'v2' ? [{ name: 'parse', reviewed: 4 }] : [{ name: 'parse', reviewed: 4 }]));
    show();
    fireEvent.click(await screen.findByRole('button', { name: 'Accuracy of: Whole run' }));
    fireEvent.click(await screen.findByRole('option', { name: /parse/ }));
    const row = await screen.findByRole('button', { name: /Accuracy · parse/ });
    expect(row).toHaveTextContent('+50.0 pts');
    expect(api.getFieldAccuracy).toHaveBeenCalledWith('checkout', 'v3', 'parse');
  });

  it('explains when a version has no runs in the range', async () => {
    api.getWorkflowVersions.mockResolvedValue({ ...empty, items: [version('v2', 0.004, 0.002)] });
    show();
    expect(await screen.findByText('Nothing to compare in this range')).toBeInTheDocument();
  });
});
