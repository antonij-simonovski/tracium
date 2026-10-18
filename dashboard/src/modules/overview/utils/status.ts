import { relativeTime } from '../../../common';
import type { SyncTone } from '../../../common';
import type { SyncStatus } from '../components';
import type { FailureRow } from '../interfaces';

// The rows are only the top workflows, so the failed total comes from the
// envelope rather than re-summing them.
export function failuresSummary(rows: FailureRow[], total: number): { totalFailed: number; worstWorkflow: string } {
  const worst = rows.reduce<FailureRow | null>((m, r) => (!m || r.count > m.count ? r : m), null);
  return { totalFailed: total, worstWorkflow: worst?.workflow ?? '—' };
}

// Sections don't poll, so freshness is only as good as the oldest successful fetch.
const STALE_AFTER_MS = 5 * 60 * 1000;

export function syncStatus(
  queries: { isError: boolean; isSuccess: boolean; dataUpdatedAt: number }[],
  now = Date.now(),
): SyncStatus {
  if (queries.some((q) => q.isError)) return { label: 'Sync failed', tone: 'error' };
  const updates = queries.filter((q) => q.isSuccess).map((q) => q.dataUpdatedAt);
  if (updates.length === 0) return { label: 'Syncing…', tone: 'syncing' };
  const oldest = Math.min(...updates);
  const tone: SyncTone = now - oldest > STALE_AFTER_MS ? 'stale' : 'live';
  return { label: `Synced ${relativeTime(oldest, now)}`, tone };
}
