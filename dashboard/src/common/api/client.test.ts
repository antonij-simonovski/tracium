import { afterEach, describe, expect, it, vi } from 'vitest';
import { APIError, BaseAPIClient, NetworkError } from './client';
import { WorkspacesAPI } from './workspaces';
import type { APIClientConfig } from '../interfaces';

class TestClient extends BaseAPIClient {
  read(path: string, params?: Record<string, string | undefined>) {
    return this.get(path, params);
  }

  write(path: string, body: unknown) {
    return this.post(path, body);
  }
}

const config: APIClientConfig = { baseUrl: 'https://api.test', apiKey: 'tok' };

function respond(status: number, body: string) {
  return vi.fn(() => Promise.resolve(new Response(body, { status })));
}

function requestedUrl(fetchMock: ReturnType<typeof vi.fn>): URL {
  return new URL(fetchMock.mock.calls[0][0] as string);
}

afterEach(() => vi.unstubAllGlobals());

describe('BaseAPIClient', () => {
  it('turns an error body into an APIError with its code and message', async () => {
    vi.stubGlobal('fetch', respond(409, JSON.stringify({ code: 'SLUG_TAKEN', message: 'Slug already in use' })));
    const err = await new TestClient(config).read('/workspaces').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(APIError);
    expect(err).toMatchObject({ status: 409, code: 'SLUG_TAKEN', message: 'Slug already in use' });
  });

  it('falls back to a generic error when the body is not JSON', async () => {
    vi.stubGlobal('fetch', respond(502, '<html>Bad gateway</html>'));
    const err = await new TestClient(config).read('/workspaces').catch((e: unknown) => e);
    expect(err).toMatchObject({ status: 502, code: 'UNKNOWN_ERROR', message: 'Request failed with status 502' });
  });

  it('scopes reads to the active workspace unless one is passed explicitly', async () => {
    const fetchMock = respond(200, '[]');
    vi.stubGlobal('fetch', fetchMock);
    const client = new TestClient({ ...config, workspaceId: 'ws-1' });

    await client.read('/traces', { range: '7d' });
    expect(requestedUrl(fetchMock).searchParams.get('workspace_id')).toBe('ws-1');
    expect(requestedUrl(fetchMock).searchParams.get('range')).toBe('7d');

    fetchMock.mockClear();
    await client.read('/traces', { workspace_id: 'ws-2' });
    expect(requestedUrl(fetchMock).searchParams.get('workspace_id')).toBe('ws-2');
  });

  it('does not add workspace_id to writes', async () => {
    const fetchMock = respond(200, '{}');
    vi.stubGlobal('fetch', fetchMock);
    await new TestClient({ ...config, workspaceId: 'ws-1' }).write('/workspaces', { name: 'x' });
    expect(requestedUrl(fetchMock).searchParams.has('workspace_id')).toBe(false);
  });

  it('aborts a request that exceeds the timeout', async () => {
    vi.stubGlobal('fetch', vi.fn((_url: string, init: RequestInit) => new Promise((_resolve, reject) => {
      init.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
    })));
    await expect(new TestClient({ ...config, timeoutMs: 5 }).read('/slow')).rejects.toBeInstanceOf(NetworkError);
  });

  it('encodes ids in request paths', async () => {
    const fetchMock = respond(200, '[]');
    vi.stubGlobal('fetch', fetchMock);
    await new WorkspacesAPI(config).members('a/b?c');
    expect(requestedUrl(fetchMock).pathname).toBe('/v1/workspaces/a%2Fb%3Fc/members');
  });
});
