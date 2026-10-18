import { describe, expect, it } from 'vitest';
import { bucketLabel } from './buckets';
import { deltaParts } from './deltas';
import { fmtCost, fmtDelta, plural } from './formatters';
import { relativeTime } from './time';

describe('bucketLabel', () => {
  it('reads daily buckets as UTC dates', () => {
    const midnightUtc = Date.UTC(2026, 9, 6);
    expect(bucketLabel(midnightUtc, '7d')).toBe('Tue 10/6');
    expect(bucketLabel(midnightUtc, '30d')).toBe('10/6');
  });

  it('reads hourly buckets in local time', () => {
    expect(bucketLabel(new Date(2026, 9, 6, 9).getTime(), '24h')).toBe('09:00');
  });
});

describe('fmtCost', () => {
  it('shows a real zero as $0.00 and tiny amounts as a floor', () => {
    expect(fmtCost(0)).toBe('$0.00');
    expect(fmtCost(0.0004)).toBe('< $0.001');
    expect(fmtCost(0.0042)).toBe('$0.0042');
    expect(fmtCost(0.25)).toBe('$0.25');
    expect(fmtCost(12.5)).toBe('$12.50');
  });
});

describe('fmtDelta', () => {
  it('signs by the fraction and never prints a signed zero', () => {
    expect(fmtDelta(0.25)).toBe('+25%');
    expect(fmtDelta(-0.004)).toBe('-0.4%');
    expect(fmtDelta(0.0004)).toBe('0%');
    expect(fmtDelta(-0.0004)).toBe('0%');
    expect(fmtDelta(0)).toBe('0%');
  });
});

describe('plural', () => {
  it('adds an s to every count but one', () => {
    expect(plural(1, 'span')).toBe('1 span');
    expect(plural(0, 'span')).toBe('0 spans');
    expect(plural(1200, 'run')).toBe('1,200 runs');
  });
});

describe('deltaParts', () => {
  it('shows no figure when there is no baseline', () => {
    expect(deltaParts({ value: 1, delta: 0, delta_type: 'neutral' })).toEqual({ delta: '', deltaTone: 'neutral', hint: 'no change' });
  });

  it('keeps the tone from the API', () => {
    expect(deltaParts({ value: 1, delta: -0.1, delta_type: 'bad' })).toEqual({ delta: '-10%', deltaTone: 'bad', hint: 'vs prev period' });
  });
});

describe('relativeTime', () => {
  const now = Date.UTC(2026, 9, 6, 12);
  it('rounds down to the largest whole unit', () => {
    expect(relativeTime(now - 5_000, now)).toBe('just now');
    expect(relativeTime(now - 42_000, now)).toBe('42s ago');
    expect(relativeTime(now - 119_000, now)).toBe('1m ago');
    expect(relativeTime(now - 5 * 3_600_000, now)).toBe('5h ago');
    expect(relativeTime(now - 3 * 86_400_000, now)).toBe('3d ago');
    expect(relativeTime(now + 5_000, now)).toBe('just now');
  });
});
