import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import ApiKeysPage from './ApiKeysPage';

const api = vi.hoisted(() => ({ list: vi.fn(), create: vi.fn(), revoke: vi.fn() }));
vi.mock('../../../common/providers/APIProvider', () => ({
  useAPIClient: () => ({
    apiKeysAPI: { list: api.list, create: api.create, revoke: api.revoke },
  }),
}));

function show(workspaceId: string | undefined = 'ws-1', workspaceName?: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <ApiKeysPage workspaceId={workspaceId} workspaceName={workspaceName} />
    </QueryClientProvider>,
  );
}

const KEY = {
  id: 'k1',
  workspace_id: 'ws-1',
  created_by: 'demo@tracium.ai',
  name: 'Production ingest',
  prefix: 'trc_9f3a1b2c',
  scope: 'ingest' as const,
  created_at: '2026-03-12T10:00:00Z',
  last_used_at: null,
  revoked_at: null,
};

beforeEach(() => {
  api.list.mockReset();
  api.create.mockReset();
  api.revoke.mockReset();
});

describe('live API keys page', () => {
  it('lists the workspace keys from the backend', async () => {
    api.list.mockResolvedValue([KEY]);
    show();
    expect(await screen.findByText('Production ingest')).toBeInTheDocument();
    expect(api.list).toHaveBeenCalledWith('ws-1');
    expect(screen.getByText(/trc_9f3a1b2c/)).toBeInTheDocument();
  });

  it('shows an empty state when the workspace has no keys', async () => {
    api.list.mockResolvedValue([]);
    show();
    expect(await screen.findByText(/No API keys yet/)).toBeInTheDocument();
  });

  it('creates a key and reveals the one-time token', async () => {
    api.list.mockResolvedValue([]);
    api.create.mockResolvedValue({ key: KEY, token: 'trc_secret_full_token' });
    show();
    await screen.findByText(/No API keys yet/);

    fireEvent.click(screen.getByRole('button', { name: /Create key/ }));
    fireEvent.change(screen.getByPlaceholderText(/Production ingest/), {
      target: { value: 'CI runner' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));

    expect(await screen.findByText('trc_secret_full_token')).toBeInTheDocument();
    expect(api.create).toHaveBeenCalledWith('ws-1', 'CI runner');
  });

  it('drafts the key in a row that Esc cancels and Done closes', async () => {
    api.list.mockResolvedValue([]);
    api.create.mockResolvedValue({ key: KEY, token: 'trc_secret_full_token' });
    show('ws-1', 'Acme');
    await screen.findByText(/No API keys yet/);
    expect(screen.queryByPlaceholderText(/Production ingest/)).not.toBeInTheDocument();

    const open = screen.getByRole('button', { name: /Create key/ });
    fireEvent.click(open);
    expect(open).toBeDisabled();
    expect(screen.getByText('Ingests to Acme')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Create' })).toBeDisabled();
    fireEvent.keyDown(screen.getByPlaceholderText(/Production ingest/), { key: 'Escape' });
    expect(screen.queryByPlaceholderText(/Production ingest/)).not.toBeInTheDocument();
    expect(open).toBeEnabled();

    fireEvent.click(open);
    fireEvent.change(screen.getByPlaceholderText(/Production ingest/), { target: { value: 'CI' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));
    await screen.findByText('trc_secret_full_token');
    fireEvent.click(screen.getByRole('button', { name: 'Done' }));
    expect(screen.queryByText('trc_secret_full_token')).not.toBeInTheDocument();
    expect(open).toBeEnabled();
  });

  it('does not report a successful copy when the clipboard write fails', async () => {
    api.list.mockResolvedValue([]);
    api.create.mockResolvedValue({ key: KEY, token: 'trc_secret_full_token' });
    const writeText = vi.fn().mockRejectedValue(new Error('denied'));
    Object.assign(navigator, { clipboard: { writeText } });

    show();
    await screen.findByText(/No API keys yet/);
    fireEvent.click(screen.getByRole('button', { name: /Create key/ }));
    fireEvent.change(screen.getByPlaceholderText(/Production ingest/), {
      target: { value: 'CI runner' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));
    await screen.findByText('trc_secret_full_token');

    fireEvent.click(screen.getByRole('button', { name: 'Copy' }));

    // The write was attempted, but the UI must not claim success.
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('trc_secret_full_token'));
    expect(await screen.findByRole('button', { name: 'Copy failed' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Copied' })).not.toBeInTheDocument();
  });

  it('revokes a key through the backend', async () => {
    api.list.mockResolvedValue([KEY]);
    api.revoke.mockResolvedValue(undefined);
    show();
    await screen.findByText('Production ingest');

    fireEvent.click(screen.getByRole('button', { name: 'Revoke Production ingest' }));
    fireEvent.click(screen.getByRole('button', { name: /Revoke key/ }));

    await waitFor(() => expect(api.revoke).toHaveBeenCalledWith('ws-1', 'k1'));
  });

  it('clears the revealed one-time token when the workspace changes', async () => {
    api.list.mockResolvedValue([]);
    api.create.mockResolvedValue({ key: KEY, token: 'trc_secret_full_token' });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { rerender } = render(
      <QueryClientProvider client={client}>
        <ApiKeysPage workspaceId="ws-1" />
      </QueryClientProvider>,
    );
    await screen.findByText(/No API keys yet/);

    fireEvent.click(screen.getByRole('button', { name: /Create key/ }));
    fireEvent.change(screen.getByPlaceholderText(/Production ingest/), {
      target: { value: 'CI runner' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));
    expect(await screen.findByText('trc_secret_full_token')).toBeInTheDocument();

    rerender(
      <QueryClientProvider client={client}>
        <ApiKeysPage workspaceId="ws-2" />
      </QueryClientProvider>,
    );
    expect(screen.queryByText('trc_secret_full_token')).not.toBeInTheDocument();
  });

  it('surfaces a load failure instead of inventing keys', async () => {
    api.list.mockRejectedValue(new Error('offline'));
    show();
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('Could not load API keys'),
    );
  });

  // The demo (auth-page preview) shares the live UI but runs on an in-memory
  // mock — no backend calls, yet create → reveal still works.
  it('renders an interactive demo without touching the backend', async () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <ApiKeysPage demo />
      </QueryClientProvider>,
    );
    expect(await screen.findByText('Production ingest')).toBeInTheDocument();
    expect(screen.getByText('Staging ingest')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /Create key/ }));
    fireEvent.change(screen.getByPlaceholderText(/Production ingest/), {
      target: { value: 'Local test' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));
    expect(await screen.findByText('Shown once.')).toBeInTheDocument();

    // The demo path must not hit the real API client.
    expect(api.list).not.toHaveBeenCalled();
    expect(api.create).not.toHaveBeenCalled();
  });
});
