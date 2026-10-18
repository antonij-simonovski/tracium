import { useState } from 'react';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import SettingsPage from './SettingsPage';
import type { Workspace } from '../../shell/interfaces';
import { APIError } from '../../../common/api';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

// AccountView's password-change field reads usersAPI from context; the
// 'workspace setup' tests exercise workspace setup, not the API client, so a
// mock is enough there. The 'change password' tests below override it per case.
const changePassword = vi.fn();
const workspacesAPI = {
  members: vi.fn(), invites: vi.fn(), invite: vi.fn(), revokeInvite: vi.fn(), removeMember: vi.fn(),
};
vi.mock('../../../common/providers/APIProvider', () => ({
  useAPIClient: () => ({ usersAPI: { changePassword }, workspacesAPI }),
}));

const workspace: Workspace = {
  id: 'ws_generated_123' as Workspace['id'], name: 'Acme Production',
  slug: 'acme-production', env: 'production', role: 'Owner', members: 1,
};

beforeEach(() => {
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })));
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: vi.fn().mockResolvedValue(undefined) } });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

function Flow({ create }: { create: () => Promise<Workspace> }) {
  const [selected, setSelected] = useState<Workspace | null>(null);
  const [createMode, setCreateMode] = useState(true);
  return <SettingsPage workspace={selected} createMode={createMode} onCancelCreate={() => setCreateMode(false)} createWorkspace={async () => {
    const result = await create();
    setSelected(result);
    setCreateMode(false);
    return result;
  }} />;
}

describe('workspace setup', () => {
  it('explains key-based ingest before creation and shows the exporter setup after success', async () => {
    const create = vi.fn().mockResolvedValue(workspace);
    render(<Flow create={create} />);
    expect(screen.getByText(/Applications send data with an API key/)).toBeInTheDocument();
    fireEvent.change(screen.getByRole('textbox', { name: 'Workspace name' }), { target: { value: workspace.name } });
    expect(screen.getByRole('textbox', { name: 'Slug' })).toHaveValue(workspace.slug);
    fireEvent.click(screen.getByRole('button', { name: 'Create workspace & continue' }));
    expect(await screen.findByRole('heading', { name: 'Connect your application' })).toHaveFocus();
    expect(screen.getByText(/created · Step 2 of 2/)).toBeInTheDocument();
    expect(screen.queryByText(workspace.id)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Copy configuration' }));
    await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalledWith(expect.stringContaining('OTEL_EXPORTER_OTLP_HEADERS="Authorization=Bearer YOUR_API_KEY"')));
    expect(navigator.clipboard.writeText).not.toHaveBeenCalledWith(expect.stringContaining('tracium.workspace.id'));
    expect(create).toHaveBeenCalledTimes(1);
  });

  it('prevents duplicate submissions and preserves details after a failure', async () => {
    let reject!: (error: Error) => void;
    const create = vi.fn(() => new Promise<Workspace>((_, fail) => { reject = fail; }));
    render(<Flow create={create} />);
    fireEvent.change(screen.getByRole('textbox', { name: 'Workspace name' }), { target: { value: workspace.name } });
    fireEvent.click(screen.getByRole('button', { name: 'Create workspace & continue' }));
    expect(screen.getByRole('button', { name: 'Creating…' })).toBeDisabled();
    fireEvent.submit(screen.getByRole('textbox', { name: 'Workspace name' }).closest('form')!);
    expect(create).toHaveBeenCalledTimes(1);
    await act(async () => reject(new Error('Workspace slug already exists.')));
    expect(screen.getByRole('alert')).toHaveTextContent('Workspace slug already exists.');
    expect(screen.getByRole('textbox', { name: 'Workspace name' })).toHaveValue(workspace.name);
    expect(screen.getByRole('button', { name: 'Create workspace & continue' })).toBeEnabled();
  });

  it('keeps setup available later and follows the selected workspace', async () => {
    const overview = vi.fn();
    const { rerender } = render(<SettingsPage workspace={workspace} onOpenOverview={overview} />);
    expect(screen.getByRole('heading', { name: 'Connect your application' })).toBeInTheDocument();
    expect(screen.queryByText(/created · Step 2/)).not.toBeInTheDocument();
    const other = { ...workspace, id: 'ws_other' as Workspace['id'], name: 'Staging' };
    rerender(<SettingsPage workspace={other} onOpenOverview={overview} />);
    expect(screen.getAllByText('Staging').length).toBeGreaterThan(0);
    expect(screen.queryByText(workspace.name)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Go to overview' }));
    expect(overview).toHaveBeenCalledOnce();
  });

  it('links to the API keys screen to create the ingest key', () => {
    const openKeys = vi.fn();
    render(<SettingsPage workspace={workspace} onOpenApiKeys={openKeys} />);
    fireEvent.click(screen.getByRole('button', { name: 'Create API key' }));
    expect(openKeys).toHaveBeenCalledOnce();
  });

  it('points the exporter at the local collector unless an ingest endpoint is supplied', () => {
    const { rerender } = render(<SettingsPage workspace={workspace} />);
    expect(screen.getByText(/OTEL_EXPORTER_OTLP_ENDPOINT=http:\/\/localhost:4318/)).toBeInTheDocument();
    expect(screen.getByText(/local Docker installation/)).toBeInTheDocument();
    rerender(<SettingsPage workspace={workspace} ingestEndpoint="https://ingest.example.com" />);
    expect(screen.getByText(/OTEL_EXPORTER_OTLP_ENDPOINT=https:\/\/ingest\.example\.com/)).toBeInTheDocument();
    expect(screen.queryByText(/localhost/)).not.toBeInTheDocument();
  });

  it('offers manual copying when clipboard access fails', async () => {
    vi.mocked(navigator.clipboard.writeText).mockRejectedValue(new Error('Permission denied'));
    render(<SettingsPage workspace={workspace} />);
    fireEvent.click(screen.getByRole('button', { name: 'Copy configuration' }));
    expect(await screen.findByText('Couldn’t copy. Select and copy the text manually.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Copied' })).not.toBeInTheDocument();
    expect(screen.getByText(/OTEL_EXPORTER_OTLP_HEADERS/)).toBeInTheDocument();
  });


  it('opens creation when requested while Settings is already mounted', () => {
    const create = vi.fn().mockResolvedValue(workspace);
    const { rerender } = render(<SettingsPage workspace={workspace} createWorkspace={create} />);
    fireEvent.click(screen.getByRole('button', { name: 'Account' }));
    rerender(<SettingsPage workspace={workspace} createWorkspace={create} createMode />);
    expect(screen.getByRole('heading', { name: 'Create workspace' })).toBeInTheDocument();
  });
});

describe('change password', () => {
  beforeEach(() => {
    changePassword.mockReset();
  });

  function openForm(onSessionRenewed?: (token: string) => void) {
    render(<SettingsPage workspace={workspace} onSessionRenewed={onSessionRenewed} />);
    fireEvent.click(screen.getByRole('button', { name: 'Account' }));
    fireEvent.click(screen.getByRole('button', { name: 'Change' }));
  }

  it('submits the current and new password, keeps the renewed session and shows confirmation', async () => {
    changePassword.mockResolvedValue('renewed-token');
    const onSessionRenewed = vi.fn();
    openForm(onSessionRenewed);
    fireEvent.change(screen.getByLabelText('Current password'), { target: { value: 'old-pass' } });
    fireEvent.change(screen.getByLabelText('New password'), { target: { value: 'new-password' } });
    fireEvent.change(screen.getByLabelText('Confirm new password'), { target: { value: 'new-password' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save password' }));
    await waitFor(() => expect(changePassword).toHaveBeenCalledWith('old-pass', 'new-password'));
    expect(await screen.findByText('Password updated.')).toBeInTheDocument();
    expect(onSessionRenewed).toHaveBeenCalledWith('renewed-token');
  });

  it('lets password managers tell the current and new password apart', () => {
    openForm();
    expect(screen.getByLabelText('Current password')).toHaveAttribute('autocomplete', 'current-password');
    expect(screen.getByLabelText('New password')).toHaveAttribute('autocomplete', 'new-password');
    expect(screen.getByLabelText('Confirm new password')).toHaveAttribute('autocomplete', 'new-password');
  });

  it('rejects a mismatched confirmation without calling the API', () => {
    openForm();
    fireEvent.change(screen.getByLabelText('Current password'), { target: { value: 'old-pass' } });
    fireEvent.change(screen.getByLabelText('New password'), { target: { value: 'new-password' } });
    fireEvent.change(screen.getByLabelText('Confirm new password'), { target: { value: 'something-else' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save password' }));
    expect(screen.getByRole('alert')).toHaveTextContent("New passwords don't match.");
    expect(changePassword).not.toHaveBeenCalled();
  });

  it('surfaces the API error message (e.g. wrong current password)', async () => {
    changePassword.mockRejectedValue(new APIError('Current password is incorrect', 401, 'INVALID_CURRENT_PASSWORD'));
    openForm();
    fireEvent.change(screen.getByLabelText('Current password'), { target: { value: 'wrong' } });
    fireEvent.change(screen.getByLabelText('New password'), { target: { value: 'new-password' } });
    fireEvent.change(screen.getByLabelText('Confirm new password'), { target: { value: 'new-password' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save password' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Current password is incorrect');
  });
});

describe('members', () => {
  const owner = { user_id: 'u-1', email: 'owner@acme.dev', role: 'owner', joined_at: '2026-09-01T00:00:00Z' };
  const bob = { user_id: 'u-2', email: 'bob@acme.dev', role: 'member', joined_at: '2026-09-02T00:00:00Z' };
  const pending = { id: 'inv-1', workspace_id: workspace.id, email: 'carol@acme.dev', role: 'member', invited_by: 'u-1', created_at: '2026-09-20T00:00:00Z', expires_at: '2026-09-27T00:00:00Z' };

  beforeEach(() => {
    Object.values(workspacesAPI).forEach(fn => fn.mockReset());
    workspacesAPI.members.mockResolvedValue([owner, bob]);
    workspacesAPI.invites.mockResolvedValue([pending]);
  });

  function openMembers(ws: Workspace = workspace, email = 'owner@acme.dev') {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<QueryClientProvider client={client}>
      <SettingsPage workspace={ws} account={{ email, name: 'Owner', initials: 'OW' }} />
    </QueryClientProvider>);
    fireEvent.click(screen.getByRole('button', { name: 'Members' }));
  }

  it('lists members and pending invites for the owner', async () => {
    openMembers();
    const members = await screen.findByRole('list', { name: 'Members' });
    expect(members).toHaveTextContent('owner@acme.dev');
    expect(members).toHaveTextContent('You');
    expect(members).toHaveTextContent('bob@acme.dev');
    expect(screen.getByRole('button', { name: 'Remove' })).toHaveAttribute('title', 'Remove bob@acme.dev');
    expect(await screen.findByRole('list', { name: 'Pending invites' })).toHaveTextContent('carol@acme.dev');
    expect(workspacesAPI.members).toHaveBeenCalledWith(workspace.id);
  });

  it('creates an invite link and shows it once', async () => {
    workspacesAPI.invite.mockResolvedValue({ ...pending, email: 'dave@acme.dev', token: 'trci_tok', email_sent: false });
    openMembers();
    fireEvent.change(screen.getByRole('textbox', { name: 'Invite email' }), { target: { value: ' dave@acme.dev ' } });
    fireEvent.click(screen.getByRole('button', { name: /Create invite link/ }));
    await waitFor(() => expect(workspacesAPI.invite).toHaveBeenCalledWith(workspace.id, 'dave@acme.dev'));
    expect(await screen.findByText(`${window.location.origin}/invite/trci_tok`)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Copy invite link' }));
    await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalledWith(`${window.location.origin}/invite/trci_tok`));
  });

  it('says when the invite was emailed and still offers the link', async () => {
    workspacesAPI.invite.mockResolvedValue({ ...pending, email: 'dave@acme.dev', token: 'trci_tok', email_sent: true });
    openMembers();
    fireEvent.change(screen.getByRole('textbox', { name: 'Invite email' }), { target: { value: 'dave@acme.dev' } });
    fireEvent.click(screen.getByRole('button', { name: /Create invite link/ }));
    expect(await screen.findByText(/We emailed an invite to/)).toHaveTextContent('dave@acme.dev');
    expect(screen.getByText(`${window.location.origin}/invite/trci_tok`)).toBeInTheDocument();
  });

  it('shows the member limit when the workspace is full', async () => {
    workspacesAPI.invite.mockRejectedValue(new APIError('This workspace has reached its limit of 3 members.', 403, 'MEMBER_LIMIT_REACHED'));
    openMembers();
    fireEvent.change(screen.getByRole('textbox', { name: 'Invite email' }), { target: { value: 'dave@acme.dev' } });
    fireEvent.click(screen.getByRole('button', { name: /Create invite link/ }));
    expect(await screen.findByRole('alert')).toHaveTextContent('limit of 3 members');
  });

  it('shows why an invite was refused', async () => {
    workspacesAPI.invite.mockRejectedValue(new APIError('that account is already a member of this workspace', 409, 'ALREADY_MEMBER'));
    openMembers();
    fireEvent.change(screen.getByRole('textbox', { name: 'Invite email' }), { target: { value: 'bob@acme.dev' } });
    fireEvent.click(screen.getByRole('button', { name: /Create invite link/ }));
    expect(await screen.findByRole('alert')).toHaveTextContent('already a member');
  });

  it('revokes a pending invite', async () => {
    workspacesAPI.revokeInvite.mockResolvedValue(undefined);
    openMembers();
    await screen.findByRole('list', { name: 'Pending invites' });
    fireEvent.click(screen.getByRole('button', { name: 'Revoke' }));
    await waitFor(() => expect(workspacesAPI.revokeInvite).toHaveBeenCalledWith(workspace.id, 'inv-1'));
  });

  it('is read-only for a member', async () => {
    openMembers({ ...workspace, role: 'member' }, 'bob@acme.dev');
    expect(await screen.findByRole('list', { name: 'Members' })).toHaveTextContent('owner@acme.dev');
    expect(screen.getByText(/Only the workspace owner can invite or remove members/)).toBeInTheDocument();
    expect(screen.queryByRole('textbox', { name: 'Invite email' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Remove' })).toBeNull();
    expect(workspacesAPI.invites).not.toHaveBeenCalled();
  });
});
