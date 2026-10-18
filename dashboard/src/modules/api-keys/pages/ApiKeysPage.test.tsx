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

function show(workspaceId: string | undefined = 'ws-1') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <ApiKeysPage workspaceId={workspaceId} />
    </QueryClientProvider>,
  );
}

const KEY = {
  id: 'k1',
  workspace_id: 'ws-1',
  created_by: 'demo@tracium.ai',
  name: 'Production ingest',
  prefix: 'trc_9f3a1b2c',
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
    // The dialog's submit button (last of the two "Create key" buttons).
    const createButtons = screen.getAllByRole('button', { name: /Create key/ });
    fireEvent.click(createButtons[createButtons.length - 1]);

    expect(await screen.findByText('trc_secret_full_token')).toBeInTheDocument();
    expect(api.create).toHaveBeenCalledWith('ws-1', 'CI runner');
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
    const createButtons = screen.getAllByRole('button', { name: /Create key/ });
    fireEvent.click(createButtons[createButtons.length - 1]);
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
    const createButtons = screen.getAllByRole('button', { name: /Create key/ });
    fireEvent.click(createButtons[createButtons.length - 1]);
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

    fireEvent.change(screen.getByPlaceholderText(/Production ingest/), {
      target: { value: 'Local test' },
    });
    const createButtons = screen.getAllByRole('button', { name: /Create key/ });
    fireEvent.click(createButtons[createButtons.length - 1]);
    expect(await screen.findByText(/Key created/)).toBeInTheDocument();

    // The demo path must not hit the real API client.
    expect(api.list).not.toHaveBeenCalled();
    expect(api.create).not.toHaveBeenCalled();
  });
});
