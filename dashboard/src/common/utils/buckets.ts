// Shared mapping from the metrics API's time-bucketed responses (CostBucket /
// LatencyBucket / ErrorBucket, keyed by bucket_ms) to the chart point shapes the
// chart components render (CostPoint / LatencyPoint / ErrorPoint, with a label).
// Used by both the overview and the per-workflow detail charts so the bucket → axis
// labelling stays identical between them.

import type { CostPoint, LatencyPoint, ErrorPoint } from '../interfaces';
import type { CostBucket, LatencyBucket, ErrorBucket } from '../../modules/overview/interfaces';

// bucketLabel renders a bucket's x-axis label for the active range: hourly for
// 24h, weekday + m/d for 7d (the only range with room), bare m/d for wider
// daily ranges (30d/90d/1y). Daily buckets start at UTC midnight, so their dates
// are read in UTC.
export function bucketLabel(ms: number, range: string): string {
  const d = new Date(ms);
  if (range === '24h') return `${String(d.getHours()).padStart(2, '0')}:00`;
  const day = `${d.getUTCMonth() + 1}/${d.getUTCDate()}`;
  if (range === '7d') return `${['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'][d.getUTCDay()]} ${day}`;
  return day;
}

export const toCostPoints = (buckets: CostBucket[], range: string): CostPoint[] =>
  buckets.map((b) => ({ label: bucketLabel(b.bucket_ms, range), value: b.value }));

// Latency arrives in ms; the chart and legend are in seconds. Empty buckets come
// back null and stay null (a gap), rather than collapsing to 0.
const toSeconds = (ms: number | null): number | null => (ms == null ? null : ms / 1000);
export const toLatencyPoints = (buckets: LatencyBucket[], range: string): LatencyPoint[] =>
  buckets.map((b) => ({
    label: bucketLabel(b.bucket_ms, range),
    p50: toSeconds(b.p50),
    p95: toSeconds(b.p95),
    p99: toSeconds(b.p99),
  }));

export const toErrorPoints = (buckets: ErrorBucket[], range: string): ErrorPoint[] =>
  buckets.map((b) => ({ label: bucketLabel(b.bucket_ms, range), errors: b.errors, total: b.total }));
