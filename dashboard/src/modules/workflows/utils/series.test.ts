import { describe, it, expect } from 'vitest';
import { buildCostSeries, buildLatencySeries, buildErrorSeries, buildRuns } from './series';
import type { Workflow } from '../interfaces';

const WORKFLOW: Workflow = {
  name: 'rewrite-message',
  calls: 156,
  cost: 0.3104,
  avg_latency_ms: 4800,
  error_rate: 0.083,
  trend: [],
  last_trace_id: 't_demo',
};

describe('workflow detail series', () => {
  it('is deterministic per workflow (same name → same series)', () => {
    expect(buildCostSeries(WORKFLOW)).toEqual(buildCostSeries(WORKFLOW));
    expect(buildLatencySeries(WORKFLOW)).toEqual(buildLatencySeries(WORKFLOW));
    expect(buildErrorSeries(WORKFLOW)).toEqual(buildErrorSeries(WORKFLOW));
    expect(buildRuns(WORKFLOW)).toEqual(buildRuns(WORKFLOW));
  });

  it('produces a 7-day cost series with positive values', () => {
    const series = buildCostSeries(WORKFLOW);
    expect(series).toHaveLength(7);
    for (const d of series) {
      expect(d.value).toBeGreaterThan(0);
    }
  });

  it('buckets by the selected range: 24 hourly, 7 daily, 30 daily', () => {
    expect(buildCostSeries(WORKFLOW, '24h')).toHaveLength(24);
    expect(buildCostSeries(WORKFLOW, '24h')[0].label).toBe('00:00');
    expect(buildCostSeries(WORKFLOW, '7d')).toHaveLength(7);
    expect(buildCostSeries(WORKFLOW, '30d')).toHaveLength(30);
    expect(buildLatencySeries(WORKFLOW, '24h')).toHaveLength(24);
    expect(buildErrorSeries(WORKFLOW, '30d')).toHaveLength(30);
  });

  it('keeps latency percentiles ordered p50 ≤ p95 ≤ p99', () => {
    for (const d of buildLatencySeries(WORKFLOW)) {
      expect(d.p50!).toBeLessThanOrEqual(d.p95!);
      expect(d.p95!).toBeLessThanOrEqual(d.p99!);
    }
  });

  it('never reports more errors than total runs in a bucket', () => {
    for (const d of buildErrorSeries(WORKFLOW)) {
      expect(d.errors).toBeLessThanOrEqual(d.total);
      expect(d.errors).toBeGreaterThanOrEqual(0);
    }
  });

  it('builds recent runs whose failed rows carry an error code and ok rows do not', () => {
    const runs = buildRuns(WORKFLOW);
    expect(runs.length).toBeGreaterThan(0);
    for (const r of runs) {
      if (r.status === 'failed') expect(r.err).not.toBeNull();
      else expect(r.err).toBeNull();
    }
  });
});
