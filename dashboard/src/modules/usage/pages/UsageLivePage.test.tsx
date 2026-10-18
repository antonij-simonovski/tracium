import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { UsageLivePage } from './UsageLivePage';

const api = vi.hoisted(() => ({
  getKpis: vi.fn(),
  getCostSeries: vi.fn(),
  getErrorSeries: vi.fn(),
  getModelCosts: vi.fn(),
  getTokenSeries: vi.fn(),
  getUserUsage: vi.fn(),
  getWorkflowUsage: vi.fn(),
  getAttributeKeys: vi.fn(),
  getUsageByAttribute: vi.fn(),
}));
vi.mock('../../../common/providers/APIProvider', () => ({
  useAPIClient: () => ({ metricsAPI: api }),
}));

const kpi = (value: number) => ({ value, delta: 0, delta_type: 'neutral' });
const empty = { items: [], total: 0, page: 1, page_size: 20 };

function show(range: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <UsageLivePage range={range} setView={vi.fn()} setSelected={vi.fn()} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.resetAllMocks();
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })));
  api.getKpis.mockResolvedValue({ cost: kpi(5), runs: kpi(3), error_rate: kpi(0), latency_p95: kpi(0) });
  for (const fn of [api.getCostSeries, api.getErrorSeries, api.getModelCosts, api.getTokenSeries, api.getUserUsage, api.getWorkflowUsage]) {
    fn.mockResolvedValue(empty);
  }
  api.getAttributeKeys.mockResolvedValue({ ...empty, items: ['team'] });
});

afterEach(() => vi.unstubAllGlobals());

describe('live usage attribute breakdown', () => {
  it('loads attribute keys for raw ranges', async () => {
    show('30d');
    expect(await screen.findByText('By attribute')).toBeInTheDocument();
    expect(api.getAttributeKeys).toHaveBeenCalledWith('30d');
  });

  it('skips attribute keys for rollup ranges', async () => {
    show('1y');
    expect(await screen.findByText('By client')).toBeInTheDocument();
    expect(api.getAttributeKeys).not.toHaveBeenCalled();
    expect(screen.queryByText('By attribute')).not.toBeInTheDocument();
  });
});
