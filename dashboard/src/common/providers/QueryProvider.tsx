import React from 'react';
import {
  QueryClient,
  QueryClientProvider,
  QueryCache,
  MutationCache,
} from '@tanstack/react-query';
import { APIError } from '../api';

/**
 * Holds the latest `onUnauthorized` callback so the module-level caches can
 * reach it without rebuilding the QueryClient on every render.
 */
const unauthorizedHandler: { current: (() => void) | null } = { current: null };

function handleError(error: unknown) {
  if (error instanceof APIError && error.isUnauthorized) {
    unauthorizedHandler.current?.();
  }
}

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      // Never retry an expired session — it will only 401 again.
      retry: (failureCount, error) => {
        if (error instanceof APIError && error.isUnauthorized) return false;
        return failureCount < 1;
      },
    },
  },
  queryCache: new QueryCache({ onError: handleError }),
  mutationCache: new MutationCache({ onError: handleError }),
});

/**
 * When a request fails with 401 (e.g. an expired token), `onUnauthorized` is
 * invoked so the app can clear the session and send the user back to login
 * rather than leaving them on a blank canvas.
 */
export function QueryProvider({
  children,
  onUnauthorized,
}: {
  children: React.ReactNode;
  onUnauthorized?: () => void;
}) {
  unauthorizedHandler.current = onUnauthorized ?? null;
  React.useEffect(() => () => queryClient.clear(), []);
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}
