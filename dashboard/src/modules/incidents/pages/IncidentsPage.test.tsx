import { fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { IncidentsPage } from './IncidentsPage';
import type { Incident } from '../interfaces';
import { INCIDENT } from '../utils/fixtures';

const listIncidents = vi.hoisted(() => vi.fn());
vi.mock('../../../common/providers/APIProvider', () => ({
  useAPIClient: () => ({ incidentsAPI: { listIncidents }, workspaceId: 'ws-1' }),
}));

const page = (items: Incident[]) => ({ items, total: items.length, page: 1, page_size: 50 });


function show(nav = { setView: vi.fn(), setSelected: vi.fn() }) {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <IncidentsPage {...nav} />
    </QueryClientProvider>,
  );
  return nav;
}

beforeEach(() => listIncidents.mockReset());

describe('IncidentsPage', () => {
  it('lists open incidents with subject, signals, age and likely cause', async () => {
    listIncidents.mockResolvedValue(page([INCIDENT]));
    const nav = show();
    expect(await screen.findByText(INCIDENT.title)).toBeTruthy();
    expect(screen.getByText('Critical')).toBeTruthy();
    expect(screen.getByText('1 signal')).toBeTruthy();
    expect(screen.getByText('opened 2h ago')).toBeTruthy();
    expect(screen.getByText('likely cause: v42')).toBeTruthy();
    fireEvent.click(screen.getByText('sf-truncation'));
    expect(nav.setView).toHaveBeenCalledWith('workflows');
    expect(listIncidents).toHaveBeenCalledWith('open', 1);
  });

  it('shows the empty state on Open and switches to Resolved', async () => {
    listIncidents.mockResolvedValue(page([]));
    const nav = show();
    expect(await screen.findByText('Nothing looks wrong right now')).toBeTruthy();
    fireEvent.click(screen.getByText('Alert settings'));
    expect(nav.setView).toHaveBeenCalledWith('settings');
    fireEvent.click(screen.getByRole('tab', { name: /Resolved/ }));
    expect(await screen.findByText('No resolved incidents yet.')).toBeTruthy();
    expect(listIncidents).toHaveBeenCalledWith('resolved', 1);
  });
});
