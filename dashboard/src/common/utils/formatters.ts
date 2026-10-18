export function fmtCost(usd: number): string {
  if (usd === 0) return '$0.00';
  if (usd < 0.001) return '< $0.001';
  if (usd < 0.01) return `$${usd.toFixed(4)}`;
  return `$${usd.toFixed(2)}`;
}

export function fmtTokens(n: number): string {
  if (n < 1000) return String(Math.round(n));
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}k`;
  return `${(n / 1_000_000).toFixed(1)}M`;
}

export const fmtNum = (n: number): string => n.toLocaleString();

export const plural = (n: number, noun: string): string => `${fmtNum(n)} ${noun}${n === 1 ? '' : 's'}`;

export const fmtPct =(n: number): string =>
  (n < 0.01 && n > 0 ? '<0.01' : n.toFixed(n < 10 ? 2 : 1)) + '%';

export const fmtMs = (n: number): string =>
  n >= 1000 ? (n / 1000).toFixed(2) + 's' : Math.round(n) + 'ms';

// Signed percentage for a fractional change (0.124 → "+12.4%"). Changes that
// round to zero read "0%" rather than "+0%" or "-0%"; no defined change reads "—".
export function fmtDelta(fraction: number): string {
  if (!Number.isFinite(fraction)) return '—';
  const pct = Math.round(Math.abs(fraction) * 1000) / 10;
  if (pct === 0) return '0%';
  return `${fraction > 0 ? '+' : '-'}${pct}%`;
}
