import { afterEach, describe, expect, it } from 'vitest';
import { clearPendingOAuth, readPendingOAuth } from './pending';

afterEach(() => { clearPendingOAuth(); window.history.replaceState(null, '', '/'); });

describe('readPendingOAuth', () => {
  it('keeps the request across the login page', () => {
    window.history.replaceState(null, '', '/oauth/authorize?client_id=c1&state=s');
    expect(readPendingOAuth()).toBe('?client_id=c1&state=s');
    window.history.replaceState(null, '', '/login');
    expect(readPendingOAuth()).toBe('?client_id=c1&state=s');
    clearPendingOAuth();
    expect(readPendingOAuth()).toBeNull();
  });
});
