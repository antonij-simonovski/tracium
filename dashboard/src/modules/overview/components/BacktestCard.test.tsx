import { fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { describe, expect, it, vi } from 'vitest';
import { BacktestCard } from './BacktestCard';

const getIncidentBacktest = vi.fn();
vi.mock('../../../common/providers/APIProvider', () => ({
  useAPIClient: () => ({ metricsAPI: { getIncidentBacktest }, workspaceId: 'ws-1' }),
}));

describe('BacktestCard', () => {
  it('offers to set up alerts once the replay is back', async () => {
    getIncidentBacktest.mockResolvedValue({ range: '30d', incidents: 2, by_severity: { critical: 2 }, by_metric: { cost: 2 }, items: [] });
    const onSetUpAlerts = vi.fn();
    render(
      <QueryClientProvider client={new QueryClient()}>
        <BacktestCard onSetUpAlerts={onSetUpAlerts} />
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByText('Check'));
    fireEvent.click(await screen.findByText('Set up alerts'));
    expect(onSetUpAlerts).toHaveBeenCalled();
    expect(screen.getByText(/would have alerted you 2 times/)).toBeTruthy();
  });
});
