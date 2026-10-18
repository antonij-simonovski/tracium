import { render, screen } from '@testing-library/react';
import { useQuery } from '@tanstack/react-query';
import { describe, expect, it, vi } from 'vitest';
import { QueryProvider } from './QueryProvider';
import { APIError } from '../api';

function Failing({ status }: { status: number }) {
  const { isError } = useQuery({
    queryKey: ['failing', status],
    queryFn: () => Promise.reject(new APIError('failed', status, 'ERR')),
    retry: false,
  });
  return isError ? <p>failed</p> : null;
}

describe('QueryProvider', () => {
  it('signs out when a request comes back 401', async () => {
    const onUnauthorized = vi.fn();
    render(<QueryProvider onUnauthorized={onUnauthorized}><Failing status={401} /></QueryProvider>);
    await screen.findByText('failed');
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
  });

  it('keeps the session for other failures', async () => {
    const onUnauthorized = vi.fn();
    render(<QueryProvider onUnauthorized={onUnauthorized}><Failing status={500} /></QueryProvider>);
    await screen.findByText('failed');
    expect(onUnauthorized).not.toHaveBeenCalled();
  });
});
