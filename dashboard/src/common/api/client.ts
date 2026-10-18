import type { APIClientConfig } from '../interfaces';

export const API_VERSION = 'v1';

export function apiBaseUrl(): string {
  return import.meta.env.VITE_API_URL || window.location.origin;
}

export class APIError extends Error {
  status: number;
  code: string;

  constructor(message: string, status: number, code: string) {
    super(message);
    this.name = 'APIError';
    this.status = status;
    this.code = code;
  }

  get isNotFound(): boolean {
    return this.status === 404;
  }

  get isUnauthorized(): boolean {
    return this.status === 401;
  }

  get isServerError(): boolean {
    return this.status >= 500;
  }

  get isRetryable(): boolean {
    return this.status === 429 || this.status >= 500;
  }
}

export class NetworkError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'NetworkError';
  }
}

export class ConfigError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ConfigError';
  }
}

export abstract class BaseAPIClient {
  private config: APIClientConfig;

  constructor(config: APIClientConfig) {
    if (!config.baseUrl) {
      throw new ConfigError('baseUrl is required');
    }
    if (!config.apiKey) {
      throw new ConfigError('apiKey is required');
    }
    this.config = config;
  }

  protected async get<T>(
    path: string,
    params?: Record<string, string | number | boolean | undefined>,
  ): Promise<T> {
    const url = new URL(`/${API_VERSION}${path}`, this.config.baseUrl);

    if (params) {
      for (const [key, value] of Object.entries(params)) {
        if (value !== undefined) {
          url.searchParams.set(key, String(value));
        }
      }
    }

    // Scope every read to the active workspace, when one is selected. Applied
    // here so callers never have to thread workspace_id through each method;
    // an explicit workspace_id in `params` (should one ever be passed) wins.
    if (this.config.workspaceId && !url.searchParams.has('workspace_id')) {
      url.searchParams.set('workspace_id', this.config.workspaceId);
    }

    return this.request<T>(url.toString(), {
      method: 'GET',
    });
  }

  protected async post<T>(path: string, body: unknown): Promise<T> {
    const url = new URL(`/${API_VERSION}${path}`, this.config.baseUrl);

    return this.request<T>(url.toString(), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
  }

  protected async delete<T = void>(path: string): Promise<T> {
    const url = new URL(`/${API_VERSION}${path}`, this.config.baseUrl);

    return this.request<T>(url.toString(), { method: 'DELETE' });
  }

  private async request<T>(url: string, init: RequestInit): Promise<T> {
    const timeoutMs = this.config.timeoutMs ?? 10_000;
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);

    const headers: Record<string, string> = {
      Authorization: `Bearer ${this.config.apiKey}`,
      ...(init.headers as Record<string, string> | undefined),
    };

    let response: Response;
    try {
      response = await fetch(url, {
        ...init,
        headers,
        signal: controller.signal,
      });
    } catch (err: unknown) {
      clearTimeout(timer);
      const message = err instanceof Error ? err.message : 'Network request failed';
      throw new NetworkError(message);
    }

    clearTimeout(timer);

    if (!response.ok) {
      let code = 'UNKNOWN_ERROR';
      let message = `Request failed with status ${response.status}`;
      try {
        const json = (await response.json()) as { code?: string; message?: string };
        if (json.code) code = json.code;
        if (typeof json.message === 'string') message = json.message;
      } catch {
        // ignore parse error
      }
      throw new APIError(
        message,
        response.status,
        code,
      );
    }

    if (response.status === 204) {
      return undefined as T;
    }
    return response.json() as Promise<T>;
  }
}
