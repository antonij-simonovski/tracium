import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { WorkflowDetailLivePage } from './WorkflowDetailLivePage';

const api = vi.hoisted(() => ({
  getWorkflowDetail: vi.fn(),
  getWorkflowVersions: vi.fn(),
  getCostSeries: vi.fn(),
  getLatencySeries: vi.fn(),
  getErrorSeries: vi.fn(),
  listTraces: vi.fn(),
}));
vi.mock('../../../common/providers/APIProvider', () => ({
  useAPIClient: () => ({
    metricsAPI: {
      getWorkflowDetail: api.getWorkflowDetail,
      getWorkflowVersions: api.getWorkflowVersions,
      getCostSeries: api.getCostSeries,
      getLatencySeries: api.getLatencySeries,
      getErrorSeries: api.getErrorSeries,
    },
    tracesAPI: { listTraces: api.listTraces },
  }),
}));

const empty = { items: [], total: 0, page: 1, page_size: 20 };

const nav = { setView: vi.fn(), setSelected: vi.fn() };

function show(range = '7d') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const page = (r: string, name = 'checkout') => (
    <QueryClientProvider client={client}>
      <WorkflowDetailLivePage workflowName={name} range={r} setView={nav.setView} setSelected={nav.setSelected} />
    </QueryClientProvider>
  );
  const { rerender } = render(page(range));
  return (r: string, name?: string) => rerender(page(r, name));
}

beforeEach(() => {
  vi.resetAllMocks();
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} unobserve() {} });
  api.getWorkflowDetail.mockResolvedValue({
    name: 'checkout', calls: 100, cost: 1, avg_latency_ms: 900, p95_latency_ms: 1500, error_rate: 0.13,
    input_tokens: 0, output_tokens: 0, model: 'gpt-4o', provider: '', tools: [], last_trace_id: '',
  });
  api.getWorkflowVersions.mockResolvedValue(empty);
  api.getCostSeries.mockResolvedValue(empty);
  api.getLatencySeries.mockResolvedValue(empty);
  api.getErrorSeries.mockResolvedValue(empty);
  api.listTraces.mockResolvedValue(empty);
});

afterEach(() => vi.unstubAllGlobals());

describe('live workflow detail', () => {
  it('reports the same failure count in the stat and the reliability header', async () => {
    show();
    expect(await screen.findByText('13 failures')).toBeInTheDocument();
    expect(screen.getByText('13')).toBeInTheDocument();
  });

  it('marks only the panel whose request failed', async () => {
    api.getCostSeries.mockRejectedValue(new Error('boom'));
    show();
    expect(await screen.findByText("Couldn't load spend")).toBeInTheDocument();
    expect(screen.queryByText("Couldn't load latency")).not.toBeInTheDocument();
    expect(screen.queryByText("Couldn't load runs")).not.toBeInTheDocument();
  });

  it('keeps the page up while a new range loads', async () => {
    const setRange = show('7d');
    await screen.findByText('13 failures');
    api.getWorkflowDetail.mockReturnValue(new Promise(() => {}));
    setRange('30d');
    expect(screen.getByText('13 failures')).toBeInTheDocument();
  });

  it('drops the previous workflow\'s numbers when another workflow opens', async () => {
    const rerender = show('7d');
    await screen.findByText('13 failures');
    api.getWorkflowDetail.mockReturnValue(new Promise(() => {}));
    rerender('7d', 'refunds');
    expect(screen.queryByText('13 failures')).not.toBeInTheDocument();
    expect(screen.getByRole('status', { name: 'Loading' })).toBeInTheDocument();
  });
});

const version = (v: string, first: number, cpr: number) => ({
  version: v, first_seen_ms: first, last_seen_ms: first + 1000, calls: 40, cost: 40 * cpr,
  cost_per_run: cpr, p95_latency_ms: 2000, error_rate: 0.025, tokens_per_run: 900,
});

describe('live workflow detail with versions', () => {
  beforeEach(() => {
    api.getWorkflowDetail.mockResolvedValue({
      name: 'checkout', calls: 100, cost: 1, avg_latency_ms: 900, p95_latency_ms: 1500, error_rate: 0.13,
      input_tokens: 0, output_tokens: 0, model: 'gpt-4o', provider: '', tools: [], last_trace_id: '',
    });
    api.getWorkflowVersions.mockResolvedValue({ ...empty, items: [version('v3', 2000, 0.006), version('v2', 1000, 0.004)] });
  });

  it('flags a cost-per-run rise and opens the comparison against the replaced version', async () => {
    show();
    expect(await screen.findByText('Cost per run up since v3')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Compare versions' }));
    expect(nav.setView).toHaveBeenCalledWith('compare');
    const update = nav.setSelected.mock.calls[0][0];
    expect(update({})).toEqual({ workflow: 'checkout', base: 'v2', candidate: 'v3' });
  });

  it('compares a release with the version it replaced from the table', async () => {
    show();
    fireEvent.click(await screen.findByRole('button', { name: 'vs v2' }));
    const update = nav.setSelected.mock.calls[0][0];
    expect(update({})).toEqual({ workflow: 'checkout', base: 'v2', candidate: 'v3' });
  });

  it('scopes the tiles and series to the picked version', async () => {
    show();
    fireEvent.click(await screen.findByRole('button', { name: /^Version/ }));
    fireEvent.click(screen.getByRole('option', { name: /v2/ }));
    await waitFor(() => expect(api.getCostSeries).toHaveBeenCalledWith('7d', 'checkout', undefined, 'v2'));
    expect(screen.getByText('last 7 days · v2 only')).toBeInTheDocument();
    expect(screen.getByText('1 failure')).toBeInTheDocument();
  });

  it('serves long ranges from the rollup, versions included, without latency', async () => {
    show('90d');
    expect(await screen.findByRole('button', { name: 'Compare versions' })).toBeInTheDocument();
    expect(screen.getByText("Latency isn't tracked over ranges longer than 30 days.")).toBeInTheDocument();
    expect(api.getWorkflowVersions).toHaveBeenCalledWith('checkout', '90d');
    expect(api.getLatencySeries).not.toHaveBeenCalled();
  });
});
