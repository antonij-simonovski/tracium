import { describe, it, expect } from 'vitest';
import { stateToPath, pathToState } from './routing';

describe('clients routing', () => {
  it('builds /clients paths for the clients list and detail', () => {
    expect(stateToPath('users', {})).toBe('/clients');
    expect(stateToPath('user', { user: 'acme corp' })).toBe('/clients/acme%20corp');
  });

  it('parses /clients and legacy /users links to the same views', () => {
    for (const base of ['/clients', '/users']) {
      expect(pathToState(base)).toEqual({ view: 'users', selected: {} });
      expect(pathToState(`${base}/acme%20corp`)).toEqual({ view: 'user', selected: { user: 'acme corp' } });
    }
  });
});

describe('version compare routing', () => {
  it('round-trips a compare link with encoded versions', () => {
    const selected = { workflow: 'summarize comments', base: 'v3.0.2', candidate: 'release/v3.1' };
    const path = stateToPath('compare', selected);
    expect(path).toBe('/workflows/summarize%20comments/compare/v3.0.2/release%2Fv3.1');
    expect(pathToState(path)).toEqual({ view: 'compare', selected });
  });

  it('falls back to the workflow page without both versions', () => {
    expect(stateToPath('compare', { workflow: 'checkout' })).toBe('/workflows/checkout');
    expect(pathToState('/workflows/checkout/compare/v1')).toEqual({ view: 'workflows', selected: { workflow: 'checkout' } });
  });
});
