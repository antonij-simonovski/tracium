import { describe, expect, it } from 'vitest';
import { byRelease, compare, isPatch, needText, reviewsNeeded, wilson } from './accuracy';

describe('accuracy stats', () => {
  it('bounds the wilson interval around the estimate', () => {
    const [lo, hi] = wilson(80, 100);
    expect(lo).toBeCloseTo(0.711, 2);
    expect(hi).toBeCloseTo(0.867, 2);
    expect(wilson(10, 10)[1]).toBe(1);
  });

  it('calls a change significant only when its interval excludes zero', () => {
    expect(compare(120, 148, 54, 62).significant).toBe(false);
    expect(compare(100, 200, 180, 200).significant).toBe(true);
  });

  it('estimates the reviews a change still needs', () => {
    expect(reviewsNeeded(100, 200, 180, 200)).toEqual({ a: 0, b: 0, done: true });
    expect(reviewsNeeded(80, 100, 80, 100)).toBeNull();
    const n = reviewsNeeded(80, 100, 86, 100)!;
    expect(n.done).toBe(false);
    expect(compare(80 * (1 + n.a / 100), 100 + n.a, 86 * (1 + n.b / 100), 100 + n.b).significant).toBe(true);
    expect(needText(n)).toMatch(/^\+\d+ · \+\d+$/);
  });

  it('orders releases numerically and spots patches', () => {
    expect(['v1.10.0', 'v1.2.0', 'v1.2.1'].sort(byRelease)).toEqual(['v1.2.0', 'v1.2.1', 'v1.10.0']);
    expect([isPatch('v2.1.1'), isPatch('v2.1.0'), isPatch('canary')]).toEqual([true, false, false]);
  });
});
