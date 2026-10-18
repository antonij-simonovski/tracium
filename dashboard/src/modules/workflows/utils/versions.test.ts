import { describe, it, expect } from 'vitest';
import { bucketIndex, change, currentVersion, defaultPair, overlap, previousVersion, fmtSeen, releaseMarkers } from './versions';
import type { WorkflowVersion } from '../interfaces';

const DAY = 86_400_000;

function ver(version: string, first: number, last: number): WorkflowVersion {
  return {
    version, first_seen_ms: first * DAY, last_seen_ms: last * DAY,
    calls: 10, cost: 1, cost_per_run: 0.1, p95_latency_ms: 1000, error_rate: 0, tokens_per_run: 100,
  };
}

// Most recently seen first, as the API lists them.
const VERSIONS = [ver('v3', 5, 7), ver('', 0, 6), ver('v2', 2, 6), ver('v1', 0, 3)];

describe('workflow versions', () => {
  it('shows rollup days without a time of day', () => {
    expect(fmtSeen(Date.UTC(2026, 9, 3))).not.toMatch(/\d:\d/);
    expect(fmtSeen(Date.UTC(2026, 9, 3, 14, 30))).toMatch(/\d:\d/);
  });

  it('pairs the current version with the one it replaced', () => {
    expect(defaultPair(VERSIONS)).toEqual(['v2', 'v3']);
    expect(defaultPair([ver('v1', 0, 3)])).toBeNull();
  });

  it('keeps the newest release current through straggler runs of an older one', () => {
    // v1 ran an hour after v2 last did: still v2.
    expect(currentVersion([ver('v1', 0, 7.04), ver('v2', 6, 7)])?.version).toBe('v2');
    // Rolled back: v2 stopped two days before v1's latest run.
    expect(currentVersion([ver('v1', 0, 9), ver('v2', 6, 7)])?.version).toBe('v1');
    expect(currentVersion([ver('', 0, 9)])).toBeUndefined();
  });

  it('skips the unversioned runs when finding the previous version', () => {
    expect(previousVersion(VERSIONS, VERSIONS[0])?.version).toBe('v2');
    expect(previousVersion(VERSIONS, VERSIONS[3])).toBeUndefined();
  });

  it('finds the span two versions ran together', () => {
    expect(overlap(VERSIONS[0], VERSIONS[2])).toEqual([5 * DAY, 6 * DAY]);
    expect(overlap(VERSIONS[0], VERSIONS[3])).toBeNull();
  });

  it('has no relative change from a zero base', () => {
    expect(change(0.06, 0.04)).toBeCloseTo(0.5);
    expect(change(0.05, 0)).toBeNaN();
  });

  it('places a timestamp in its bucket', () => {
    const buckets = [0, DAY, 2 * DAY];
    expect(bucketIndex(buckets, -1)).toBe(-1);
    expect(bucketIndex(buckets, DAY + 5)).toBe(1);
    expect(bucketIndex(buckets, 9 * DAY)).toBe(2);
  });

  it('marks only versions first seen after the window opened', () => {
    const buckets = Array.from({ length: 8 }, (_, i) => i * DAY);
    expect(releaseMarkers(VERSIONS, buckets).map((m) => [m.label, m.index])).toEqual([['v3', 5], ['v2', 2]]);
  });
});
