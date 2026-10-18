// Ranges served from the API's daily rollup (keep in sync with UseRollup). The
// rollup can't reconstruct per-trace durations, so latency is unavailable there.
export const LONG_RANGES = ['90d', '1y'] as const;

export function isLongRange(range: string): boolean {
  return (LONG_RANGES as readonly string[]).includes(range);
}

export const RANGE_LABEL: Record<string, string> = {
  '24h': 'last 24 hours',
  '7d': 'last 7 days',
  '30d': 'last 30 days',
  '90d': 'last 90 days',
  '1y': 'last year',
};

export function rangeLabel(range: string): string {
  return RANGE_LABEL[range] ?? RANGE_LABEL['7d'];
}

export function periodLabel(range: string): string {
  const label = rangeLabel(range);
  return label[0].toUpperCase() + label.slice(1);
}
