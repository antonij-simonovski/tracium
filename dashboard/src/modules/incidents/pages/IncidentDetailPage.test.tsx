import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { IncidentDetailPage } from './IncidentDetailPage';
import { INCIDENT } from '../utils/fixtures';

const api = vi.hoisted(() => ({ getIncident: vi.fn(), resolveIncident: vi.fn(), muteIncident: vi.fn(), getWorkflowVersions: vi.fn() }));
vi.mock('../../../common/providers/APIProvider', () => ({
  useAPIClient: () => ({
    incidentsAPI: { getIncident: api.getIncident, resolveIncident: api.resolveIncident, muteIncident: api.muteIncident },
    metricsAPI: { getWorkflowVersions: api.getWorkflowVersions },
    workspaceId: 'ws-1',
  }),
}));

function show() {
  const nav = { setView: vi.fn(), setSelected: vi.fn(), setRange: vi.fn() };
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <IncidentDetailPage incidentId="inc-1" {...nav} />
    </QueryClientProvider>,
  );
  return nav;
}

beforeEach(() => {
  vi.resetAllMocks();
  api.getWorkflowVersions.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 50 });
  api.muteIncident.mockResolvedValue({});
});

describe('IncidentDetailPage', () => {
  it('shows the likely cause, signals and example runs', async () => {
    api.getIncident.mockResolvedValue(INCIDENT);
    const nav = show();
    expect(await screen.findByText('Likely cause')).toBeTruthy();
    expect(screen.getByText('Truncated calls rose')).toBeTruthy();
    fireEvent.click(screen.getByText('t1 →'));
    expect(nav.setView).toHaveBeenCalledWith('trace');
    fireEvent.click(screen.getByText('See all flagged runs'));
    expect(nav.setRange).toHaveBeenCalledWith('24h');
    const update = nav.setSelected.mock.lastCall![0];
    expect(update({})).toEqual({ workflow: 'sf-truncation', runsFlag: 'truncated' });
  });

  it('leaves out the likely cause without a version change', async () => {
    api.getIncident.mockResolvedValue({ ...INCIDENT, version_change: null });
    show();
    await screen.findByText('Truncated calls rose');
    expect(screen.queryByText('Likely cause')).toBeNull();
  });

  it('mutes only the signal type when the box is ticked', async () => {
    api.getIncident.mockResolvedValue(INCIDENT);
    show();
    fireEvent.click(await screen.findByRole('button', { name: /Mute/ }));
    fireEvent.click(screen.getByLabelText(/Only truncated calls/));
    fireEvent.click(screen.getByRole('menuitem', { name: 'For 7 days' }));
    await waitFor(() => expect(api.muteIncident).toHaveBeenCalledWith('inc-1', { duration: '7d', metric: 'truncation_rate' }));
    fireEvent.click(screen.getByRole('button', { name: /Mute/ }));
    fireEvent.click(screen.getByLabelText(/Only truncated calls/));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Forever' }));
    await waitFor(() => expect(api.muteIncident).toHaveBeenLastCalledWith('inc-1', { duration: 'forever', metric: '' }));
  });
});
