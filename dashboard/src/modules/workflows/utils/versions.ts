import { fmtCost } from '../../../common/utils/formatters';
import type { ChartMarker } from '../../../common/interfaces';
import type { WorkflowVersion } from '../interfaces';

export const versionLabel = (version: string): string => version || 'unversioned';

// Per-run costs sit around cents, where two decimals hide the change.
export const fmtRunCost = (usd: number): string => (usd >= 0.01 && usd < 1 ? `$${usd.toFixed(4)}` : fmtCost(usd));

const DAY_MS = 86_400_000;

// Long ranges come from the daily rollup as UTC midnights, which carry no time of day.
export const fmtSeen = (ms: number): string =>
  ms % DAY_MS === 0
    ? new Date(ms).toLocaleDateString(undefined, { month: 'short', day: 'numeric', timeZone: 'UTC' })
    : new Date(ms).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });

// Changes within ±5% read as noise and stay neutral.
const NOISE = 0.05;

// Tone for a change where a rise is bad (cost, latency, errors, tokens).
export function deltaColor(fraction: number): string {
  if (!Number.isFinite(fraction) || Math.abs(fraction) < NOISE) return 'var(--muted)';
  return fraction > 0 ? 'var(--error)' : 'var(--accent)';
}

// NaN when prev is 0: a rise from nothing has no relative size.
export function change(next: number, prev: number): number {
  return prev ? (next - prev) / prev : NaN;
}

// Error-rate changes read in percentage points, not relative percent.
export function fmtPts(next: number, prev: number): string {
  const pts = (next - prev) * 100;
  return `${pts >= 0 ? '+' : ''}${pts.toFixed(1)} pts`;
}

// Tone for an error-rate change: within half a point reads as noise.
export const ptsColor = (next: number, prev: number): string => deltaColor((next - prev) * 10);

export const comparable = (v: WorkflowVersion): boolean => v.version !== '';

// The version v replaced: the latest one first seen before it.
export function previousVersion(versions: WorkflowVersion[], v: WorkflowVersion): WorkflowVersion | undefined {
  return versions
    .filter((o) => comparable(o) && o.version !== v.version && o.first_seen_ms <= v.first_seen_ms)
    .reduce<WorkflowVersion | undefined>((best, o) => (!best || o.first_seen_ms > best.first_seen_ms ? o : best), undefined);
}

// The newest release still running: a straggler run of an old version after a
// rollout doesn't make it current, while a rollback does once the newer one stops
// for a day.
export function currentVersion(versions: WorkflowVersion[]): WorkflowVersion | undefined {
  const live = versions.filter(comparable);
  const latest = Math.max(...live.map((v) => v.last_seen_ms));
  return live
    .filter((v) => v.last_seen_ms >= latest - DAY_MS)
    .reduce<WorkflowVersion | undefined>((best, v) => (!best || v.first_seen_ms > best.first_seen_ms ? v : best), undefined);
}

// The current version and the one it replaced, as a [base, candidate] pair.
export function defaultPair(versions: WorkflowVersion[]): [string, string] | null {
  const current = currentVersion(versions);
  const prev = current && previousVersion(versions, current);
  return current && prev ? [prev.version, current.version] : null;
}

// The span both versions ran in, or null when one replaced the other outright.
export function overlap(a: WorkflowVersion, b: WorkflowVersion): [number, number] | null {
  const start = Math.max(a.first_seen_ms, b.first_seen_ms);
  const end = Math.min(a.last_seen_ms, b.last_seen_ms);
  return start <= end ? [start, end] : null;
}

// Index of the bucket holding ms, given bucket starts in ascending order; -1 when
// ms falls before the first bucket.
export function bucketIndex(bucketsMs: number[], ms: number): number {
  let idx = -1;
  for (let i = 0; i < bucketsMs.length && bucketsMs[i] <= ms; i++) idx = i;
  return idx;
}

// One marker per version first seen inside the window. A version already running
// when the window opened lands in the first bucket and isn't a release here.
export function releaseMarkers(
  versions: WorkflowVersion[],
  bucketsMs: number[],
  color: (v: WorkflowVersion) => string = () => 'var(--info)',
  onClick?: (v: WorkflowVersion) => void,
): ChartMarker[] {
  return versions.filter(comparable).flatMap((v) => {
    const index = bucketIndex(bucketsMs, v.first_seen_ms);
    if (index <= 0) return [];
    return [{ index, color: color(v), label: v.version, onClick: onClick && (() => onClick(v)) }];
  });
}
