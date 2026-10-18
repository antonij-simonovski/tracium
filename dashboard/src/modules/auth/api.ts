import { apiBaseUrl, APIError } from '../../common/api/client';

export interface LoginRequest {
  email: string;
  password: string;
}

export interface LoginResponse {
  token: string;
}

/**
 * Outcome of a registration. The hosted service withholds the session token
 * until the account confirms its email (confirmationRequired), so token is null
 * in that case. The standalone application issues a token immediately.
 */
export interface RegisterResult {
  token: string | null;
  confirmationRequired: boolean;
}

export class AuthError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.name = 'AuthError';
    this.status = status;
  }
}

const GENERIC_ERROR = 'Something went wrong. Please try again.';

// Picks the message for a failed request by HTTP status, falling back to the
// given text or, without one, the error's own message.
export function requestErrorMessage(err: unknown, byStatus: Record<number, string>, fallback?: string): string {
  const status = err instanceof AuthError || err instanceof APIError ? err.status : 0;
  return byStatus[status] ?? fallback ?? (err instanceof Error ? err.message : GENERIC_ERROR);
}

/** Calls an endpoint that needs no session, throwing AuthError on failure. */
export async function publicRequest(path: string, init: RequestInit, failureLabel: string): Promise<Response> {
  const response = await fetch(`${apiBaseUrl()}${path}`, init);

  if (!response.ok) {
    let message = `${failureLabel} (${response.status})`;
    try {
      const body = await response.json() as { message?: string };
      if (body.message) message = body.message;
    } catch {
      // ignore parse errors
    }
    throw new AuthError(message, response.status);
  }

  return response;
}

function postCredentials(path: string, req: LoginRequest | RegisterRequest, failureLabel: string): Promise<Response> {
  return publicRequest(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  }, failureLabel);
}

export async function loginUser(req: LoginRequest): Promise<LoginResponse> {
  const response = await postCredentials('/v1/auth/login', req, 'Login failed');
  return response.json() as Promise<LoginResponse>;
}

/** extensions carries values from DashboardExtensions.signupFields; the core ignores them. */
export interface RegisterRequest extends LoginRequest {
  extensions?: Record<string, string>;
}

export async function registerUser(req: RegisterRequest): Promise<RegisterResult> {
  const response = await postCredentials('/v1/auth/register', req, 'Sign up failed');
  // 202 Accepted: the account was created but must confirm its email before it
  // can sign in, so no token is issued. 201 Created: a token was returned.
  if (response.status === 202) {
    return { token: null, confirmationRequired: true };
  }
  const body = await response.json() as LoginResponse;
  return { token: body.token, confirmationRequired: false };
}
