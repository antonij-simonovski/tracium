import { describe, it, expect } from 'vitest';
import { failuresSummary, syncStatus } from './status';

describe('failuresSummary', () => {
  it('takes the total from the envelope and names the workflow with most failures', () => {
    const rows = [
      { workflow: 'a', count: 2, pct: 0.5 },
      { workflow: 'b', count: 5, pct: 0.1 },
    ];
    expect(failuresSummary(rows, 40)).toEqual({ totalFailed: 40, worstWorkflow: 'b' });
  });

  it('falls back to a dash when no workflow failed', () => {
    expect(failuresSummary([], 0)).toEqual({ totalFailed: 0, worstWorkflow: '—' });
  });
});

describe('syncStatus', () => {
  const now = 1_000_000_000;
  const ok = (ageMs: number) => ({ isError: false, isSuccess: true, dataUpdatedAt: now - ageMs });
  const pending = { isError: false, isSuccess: false, dataUpdatedAt: 0 };

  it('reports a failure if any query errored', () => {
    expect(syncStatus([ok(0), { ...pending, isError: true }], now)).toEqual({ label: 'Sync failed', tone: 'error' });
  });

  it('is syncing until a query succeeds', () => {
    expect(syncStatus([pending], now)).toEqual({ label: 'Syncing…', tone: 'syncing' });
  });

  it('dates freshness from the oldest successful fetch', () => {
    expect(syncStatus([ok(0), ok(30_000), pending], now)).toEqual({ label: 'Synced 30s ago', tone: 'live' });
    expect(syncStatus([ok(0), ok(6 * 60_000)], now)).toEqual({ label: 'Synced 6m ago', tone: 'stale' });
  });
});
