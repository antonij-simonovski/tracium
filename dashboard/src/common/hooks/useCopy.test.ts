import { act, renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { useCopy } from './useCopy';

function stubClipboard(writeText: () => Promise<void>) {
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
}

afterEach(() => {
  vi.useRealTimers();
});

describe('useCopy', () => {
  it('reports a copy only once the write resolves, then resets', async () => {
    vi.useFakeTimers();
    stubClipboard(vi.fn().mockResolvedValue(undefined));
    const { result } = renderHook(() => useCopy());
    await act(() => result.current[1]('hello'));
    expect(result.current[0]).toBe('copied');
    act(() => vi.advanceTimersByTime(2000));
    expect(result.current[0]).toBe('idle');
  });

  it('keeps a failure until the next attempt', async () => {
    vi.useFakeTimers();
    stubClipboard(vi.fn().mockRejectedValue(new Error('denied')));
    const { result } = renderHook(() => useCopy());
    await act(() => result.current[1]('hello'));
    act(() => vi.advanceTimersByTime(5000));
    expect(result.current[0]).toBe('failed');
  });
});
