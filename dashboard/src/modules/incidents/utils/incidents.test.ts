import { describe, expect, it } from 'vitest';
import { groupByMetric, signalLabel, signalValues, workflowStatus } from './incidents';
import type { Incident, IncidentSignal } from '../interfaces';
import type { Severity } from '../../../common';

const inc = (subject: string, severity: Incident['severity'], scope: Incident['scope'] = 'workflow') => ({ subject, severity, scope }) as Incident;

describe('workflowStatus', () => {
  it('maps each workflow to its worst open severity and skips info and other scopes', () => {
    expect(workflowStatus([
      inc('a', 'warning'), inc('a', 'critical'), inc('b', 'warning'), inc('c', 'info'), inc('d', 'critical', 'client'),
    ])).toEqual({ a: 'critical', b: 'warning' });
  });
});

describe('signalValues', () => {
  it('reads each signal in its own units', () => {
    expect(signalValues({ metric: 'budget', observed: 41.5, expected: 50 })).toEqual(['$41.50', 'of $50.00']);
    expect(signalValues({ metric: 'review_accuracy', observed: 0.4, expected: 0.933 })).toEqual(['40.0%', 'vs 93.3% on the previous version']);
    expect(signalValues({ metric: 'release_regression:truncation_rate', observed: 0.42, expected: 0.1 })).toEqual(['42.0% new version', 'vs 10.0% previous']);
    expect(signalValues({ metric: 'release_regression:cost_per_run', observed: 0.0123, expected: 0.004 })[0]).toMatch(/^\$/);
    expect(signalValues({ metric: 'ingest_silence', observed: 7, expected: 1 })).toEqual(['7h', 'vs 1h']);
  });
});

describe('groupByMetric', () => {
  it('keeps one entry per metric, the strongest first', () => {
    const s = (bucket_ms: number, score: number, severity: Severity = 'warning') =>
      ({ metric: 'cost', bucket_ms, score, severity, observed: 0, expected: 0, direction: 'spike', summary: '', trace_ids: null }) as IncidentSignal;
    const groups = groupByMetric([s(1, 3), s(2, 2), s(3, 1, 'critical')]);
    expect(groups).toHaveLength(1);
    expect(groups[0][1].map((x) => x.bucket_ms)).toEqual([3, 2, 1]);
    expect(signalLabel('budget')).toBe('Budget spent');
    expect(signalLabel('stuck_runs')).toBe('Stuck runs');
  });
});
