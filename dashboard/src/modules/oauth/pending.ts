// sessionStorage keeps the request for this tab only, across the login page.
export const OAUTH_KEY = 'tracium_oauth';

export function readPendingOAuth(): string | null {
  const fromPath = window.location.pathname.replace(/\/$/, '') === '/oauth/authorize' ? window.location.search : null;
  try {
    if (fromPath) {
      sessionStorage.setItem(OAUTH_KEY, fromPath);
      return fromPath;
    }
    return sessionStorage.getItem(OAUTH_KEY);
  } catch {
    return fromPath;
  }
}

export function clearPendingOAuth(): void {
  try {
    sessionStorage.removeItem(OAUTH_KEY);
  } catch {
    // Storage unavailable; nothing was remembered.
  }
}
