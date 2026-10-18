import { StrictMode } from 'react';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Dashboard } from './Dashboard';
import type { Workspace } from '../interfaces';

const list = vi.hoisted(() => vi.fn());
const pageError = vi.hoisted(() => ({ throwOnOverview: false }));

vi.mock('../../../common/providers/APIProvider', () => ({
  useAPIClient: () => ({ workspacesAPI: { list }, setWorkspaceId: () => {} }),
}));
vi.mock('../../overview', () => ({
  OverviewPage: () => null,
  OverviewLivePage: () => {
    if (pageError.throwOnOverview) throw new Error('overview broke');
    return <p>overview page</p>;
  },
}));
vi.mock('../../workflows', () => ({
  WorkflowsPage: () => null,
  WorkflowsLivePage: () => <p>workflows page</p>,
  WorkflowDetailDemoPage: () => null,
  WorkflowDetailLivePage: () => null,
  WORKFLOWS: [],
}));
vi.mock('../../trace-explorer', () => ({
  TraceDetailView: ({ traceId }: { traceId: string }) => <p>{`trace ${traceId}`}</p>,
}));
vi.mock('../../trace-inspector', () => ({ TraceDetailDashPage: () => null }));
vi.mock('../../usage', () => ({
  UsersPage: () => null,
  UsersLivePage: () => null,
  UserDetailPage: () => null,
  UsagePage: () => null,
  UsageLivePage: () => null,
  USERS: [],
}));
vi.mock('../../usage/pages/UserDetailLivePage', () => ({ UserDetailLivePage: () => null }));
vi.mock('../../api-keys', () => ({ ApiKeysPage: () => null }));
vi.mock('../../settings', () => ({
  SettingsPage: ({ workspace }: { workspace: Workspace | null }) => (
    <p>{`settings for ${workspace?.name} with ${workspace?.members} members`}</p>
  ),
}));

const WS: Workspace = {
  id: 'ws-1' as Workspace['id'], name: 'Acme', slug: 'acme', role: 'owner', members: 1, env: 'production',
};

let client: QueryClient;

function show() {
  return render(
    <StrictMode>
      <QueryClientProvider client={client}>
        <Dashboard />
      </QueryClientProvider>
    </StrictMode>,
  );
}

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  list.mockReset().mockResolvedValue([WS]);
  pageError.throwOnOverview = false;
  localStorage.clear();
  sessionStorage.clear();
  window.history.replaceState(null, '', '/');
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })));
});
afterEach(() => vi.unstubAllGlobals());

describe('Dashboard', () => {
  it('opens a deep link without adding a history entry', async () => {
    window.history.replaceState(null, '', '/traces/abc');
    const before = window.history.length;
    show();
    expect(await screen.findByText('trace abc')).toBeInTheDocument();
    expect(window.location.pathname).toBe('/traces/abc');
    expect(window.history.length).toBe(before);
  });

  it('pushes in-app navigation and restores it on Back', async () => {
    show();
    await screen.findByText('overview page');
    const before = window.history.length;

    fireEvent.click(screen.getByRole('button', { name: 'Workflows' }));
    expect(await screen.findByText('workflows page')).toBeInTheDocument();
    expect(window.location.pathname).toBe('/workflows');
    expect(window.history.length).toBe(before + 1);

    act(() => window.history.back());
    expect(await screen.findByText('overview page')).toBeInTheDocument();
    await waitFor(() => expect(window.location.pathname).toBe('/'));
  });

  it('shows the refetched workspace rather than a stale copy', async () => {
    window.history.replaceState(null, '', '/settings');
    show();
    expect(await screen.findByText('settings for Acme with 1 members')).toBeInTheDocument();

    act(() => client.setQueryData(['workspaces'], [{ ...WS, members: 3 }]));
    expect(await screen.findByText('settings for Acme with 3 members')).toBeInTheDocument();
  });

  it('reports a failed workspace load instead of offering to create one', async () => {
    list.mockRejectedValue(new Error('offline'));
    show();
    expect(await screen.findByText('Could not load your workspaces')).toBeInTheDocument();
    expect(screen.queryByText('Create your first workspace')).toBeNull();
  });

  it('contains a page crash and recovers on navigation', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});
    pageError.throwOnOverview = true;
    show();
    expect(await screen.findByText('overview broke')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Workflows' }));
    expect(await screen.findByText('workflows page')).toBeInTheDocument();
    consoleError.mockRestore();
  });
});
