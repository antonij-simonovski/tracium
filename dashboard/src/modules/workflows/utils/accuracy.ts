// Below this many reviews a cell is hatched: its interval is too wide to read.
export const LOW_N = 30;
const Z = 1.96;

export const pctText = (p: number) => `${Math.round(p * 100)}%`;
export const ptsText = (d: number) => `${d >= 0 ? '+' : '−'}${Math.abs(d * 100).toFixed(1)} pts`;
const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v));

export function wilson(k: number, n: number): [number, number] {
  const p = k / n, d = 1 + Z * Z / n, c = (p + Z * Z / (2 * n)) / d;
  const h = Z * Math.sqrt(p * (1 - p) / n + Z * Z / (4 * n * n)) / d;
  return [Math.max(0, c - h), Math.min(1, c + h)];
}

export const ciText = (k: number, n: number) => {
  const [lo, hi] = wilson(k, n);
  return `${Math.round(lo * 100)}–${pctText(hi)}`;
};

export interface Change {
  d: number;
  lo: number;
  hi: number;
  z: number;
  // The 95% interval of the change excludes zero.
  significant: boolean;
}

export function compare(ka: number, na: number, kb: number, nb: number): Change {
  const pa = ka / na, pb = kb / nb, d = pb - pa;
  const se = Math.sqrt(pa * (1 - pa) / na + pb * (1 - pb) / nb) || 0.01;
  const z = d / se;
  return { d, lo: d - Z * se, hi: d + Z * se, z, significant: Math.abs(z) >= Z };
}

export interface Needed {
  a: number;
  b: number;
  done: boolean;
}

// Extra reviews, in the same base:candidate mix, for the change to clear a 95%
// interval; null when there's no difference worth resolving.
export function reviewsNeeded(ka: number, na: number, kb: number, nb: number): Needed | null {
  const { d, z } = compare(ka, na, kb, nb);
  if (Math.abs(z) >= Z) return { a: 0, b: 0, done: true };
  if (Math.abs(d) < 0.01) return null;
  const k = (Z / Math.abs(z)) ** 2;
  const more = (n: number) => Math.ceil(n * (k - 1) / 10) * 10;
  return { a: more(na), b: more(nb), done: false };
}

const short = (n: number) => (n >= 1000 ? `${(n / 1000).toFixed(1)}k` : `${n}`);
export const needText = (x: Needed | null) => (!x ? '—' : x.done ? '✓' : x.a + x.b > 2000 ? '> 2k' : `+${short(x.a)} · +${short(x.b)}`);

// Red below 80% correct, mint above.
export const heat = (p: number) =>
  p < 0.8
    ? `color-mix(in srgb, var(--error) ${Math.round(clamp((0.8 - p) / 0.2, 0, 1) * 38 + 6)}%, var(--surface))`
    : `color-mix(in srgb, var(--accent) ${Math.round(clamp((p - 0.8) / 0.2, 0, 1) * 26 + 4)}%, var(--surface))`;

export const HATCH = 'repeating-linear-gradient(135deg, rgba(255,255,255,0.06) 0 2px, transparent 2px 6px)';

// 50–100% as a CSS percent from the bottom of the interval chart.
export const axis = (p: number) => clamp((p * 100 - 50) * 2, 0, 100);

export const isPatch = (version: string) => /^v?\d+\.\d+\.[1-9]\d*$/.test(version);

export const byRelease = (a: string, b: string) => a.localeCompare(b, undefined, { numeric: true });
