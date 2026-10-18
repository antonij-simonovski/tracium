import { describe, expect, it } from 'vitest';
import type { KpiSet } from '../../overview/interfaces';
import { toDailySeries, toTokenSeries, toWorkflowSummaries, sumCost, spendTile, runsTile, tokensTile } from '.';

const kpis: KpiSet = {
  cost: { value: 12.5, delta: 0.25, delta_type: 'bad' },
  runs: { value: 5000, delta: -0.1, delta_type: 'neutral' },
  latency_p95: { value: 900, delta: 0, delta_type: 'neutral' },
  error_rate: { value: 0.1, delta: 0, delta_type: 'neutral' },
};

describe('toDailySeries', () => {
  it('pairs cost and run buckets by timestamp', () => {
    const day = Date.UTC(2026, 8, 24);
    const next = Date.UTC(2026, 8, 25);
    const series = toDailySeries(
      [{ bucket_ms: day, value: 2 }, { bucket_ms: next, value: 3 }],
      [{ bucket_ms: next, total: 7, errors: 1 }],
      '7d',
    );
    expect(series).toEqual([
      { label: '9/24', cost: 2, runs: 0 },
      { label: '9/25', cost: 3, runs: 7 },
    ]);
  });

  it('labels hourly buckets on 24h', () => {
    const hour = new Date(2026, 8, 24, 9).getTime();
    expect(toDailySeries([{ bucket_ms: hour, value: 0 }], [], '24h')[0]).toMatchObject({ label: '09:00', runs: 0 });
  });
});

describe('toTokenSeries', () => {
  it('pairs token and run buckets by timestamp', () => {
    const day = Date.UTC(2026, 8, 24);
    const next = Date.UTC(2026, 8, 25);
    const series = toTokenSeries(
      [{ bucket_ms: day, input_tokens: 1200, output_tokens: 300 }, { bucket_ms: next, input_tokens: 10, output_tokens: 2 }],
      [{ bucket_ms: next, total: 5, errors: 0 }],
      '7d',
    );
    expect(series).toEqual([
      { label: '9/24', input: 1200, output: 300, runs: 0 },
      { label: '9/25', input: 10, output: 2, runs: 5 },
    ]);
  });
});

describe('toWorkflowSummaries', () => {
  it('derives the average cost per run and guards zero runs', () => {
    const rows = toWorkflowSummaries([
      { name: 'a', model: 'm', cost: 4, cost_prev: 1, runs: 2, runs_prev: 1 },
      { name: 'b', model: 'm', cost: 1, cost_prev: 0, runs: 0, runs_prev: 0 },
    ]);
    expect(rows.map((r) => r.avg)).toEqual([2, 0]);
    expect(sumCost(rows)).toBe(5);
  });
});

describe('KPI tiles', () => {
  it('builds spend, runs and tokens tiles', () => {
    expect(spendTile(kpis)).toMatchObject({ label: 'Spend', delta: '+25%', deltaTone: 'bad' });
    expect(runsTile(kpis)).toMatchObject({ label: 'Runs', value: '5,000', delta: '' });
    expect(runsTile(kpis).hint).toContain('/ 1K runs');
    expect(tokensTile(undefined).value).toBe('—');
  });
});
