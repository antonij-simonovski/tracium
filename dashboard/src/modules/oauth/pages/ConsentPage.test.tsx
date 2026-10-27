import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import ConsentPage from './ConsentPage';

const REQUEST = '?' + new URLSearchParams({
  client_id: 'c1',
  redirect_uri: 'https://claude.ai/api/mcp/auth_callback',
  response_type: 'code',
  state: 's1',
  code_challenge: 'x'.repeat(43),
  code_challenge_method: 'S256',
}).toString();

function respond(status: number, body: unknown) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }));
}

let fetchMock: ReturnType<typeof vi.fn>;
const assign = vi.fn();
beforeEach(() => {
  fetchMock = vi.fn((_url: string, init?: RequestInit) => init?.method === 'POST'
    ? respond(200, { redirect_to: 'https://claude.ai/api/mcp/auth_callback?code=tco_1&state=s1' })
    : respond(200, { client_name: 'Claude', redirect_uri: 'https://claude.ai/api/mcp/auth_callback' }));
  vi.stubGlobal('fetch', fetchMock);
  vi.stubGlobal('location', { ...window.location, assign });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); assign.mockReset(); });

function renderPage(props: Partial<React.ComponentProps<typeof ConsentPage>> = {}) {
  return render(
    <MemoryRouter>
      <ConsentPage request={REQUEST} session={{ token: 'jwt', email: 'ada@acme.dev' }} onDismiss={vi.fn()} onSignOut={vi.fn()} {...props} />
    </MemoryRouter>,
  );
}

describe('ConsentPage', () => {
  it('names the app and where it returns to', async () => {
    renderPage();
    expect(await screen.findByRole('heading', { name: 'Connect Claude' })).toBeInTheDocument();
    expect(screen.getByText('claude.ai')).toBeInTheDocument();
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toContain('/v1/oauth/authorize?client_id=c1&redirect_uri=');
    expect(init).toMatchObject({ headers: { Authorization: 'Bearer jwt' } });
  });

  it('approves and follows the redirect without leaving the page first', async () => {
    const onDismiss = vi.fn();
    sessionStorage.setItem('tracium_oauth', REQUEST);
    renderPage({ onDismiss });
    fireEvent.click(await screen.findByRole('button', { name: 'Allow' }));
    await waitFor(() => expect(assign).toHaveBeenCalledWith('https://claude.ai/api/mcp/auth_callback?code=tco_1&state=s1'));
    expect(onDismiss).not.toHaveBeenCalled();
    expect(sessionStorage.getItem('tracium_oauth')).toBeNull();
    const [, init] = fetchMock.mock.calls.find(([, i]) => i?.method === 'POST')!;
    expect(JSON.parse(init.body)).toMatchObject({ client_id: 'c1', state: 's1', code_challenge_method: 'S256', approve: true });
  });

  it('sends a denial back to the app', async () => {
    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(assign).toHaveBeenCalled());
    const [, init] = fetchMock.mock.calls.find(([, i]) => i?.method === 'POST')!;
    expect(JSON.parse(init.body)).toMatchObject({ approve: false });
  });

  it('lets the user skip connecting', async () => {
    const onDismiss = vi.fn();
    renderPage({ onDismiss });
    fireEvent.click(await screen.findByRole('button', { name: 'Go to dashboard' }));
    expect(onDismiss).toHaveBeenCalled();
    expect(assign).not.toHaveBeenCalled();
  });

  it('never redirects for an unregistered redirect', async () => {
    fetchMock.mockImplementation(() => respond(400, { code: 'INVALID_REDIRECT_URI', message: 'no' }));
    renderPage();
    expect(await screen.findByRole('alert')).toHaveTextContent('isn’t valid');
    expect(screen.queryByRole('button', { name: 'Allow' })).toBeNull();
    expect(assign).not.toHaveBeenCalled();
  });

  it('offers to sign in again when the session expired', async () => {
    fetchMock.mockImplementation(() => respond(401, { code: 'UNAUTHORIZED', message: 'expired' }));
    const onSignOut = vi.fn();
    renderPage({ onSignOut });
    fireEvent.click(await screen.findByRole('button', { name: 'Sign in again' }));
    expect(onSignOut).toHaveBeenCalled();
  });
});
