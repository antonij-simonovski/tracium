import { describe, it, expect } from 'vitest';
import { SEVERITY_META } from '../../../common';
import type { Anomaly } from '../interfaces';
import { anomalyChips, anomalyValue, bySeverity, groupIncidents, ratioLabel, toChartMarkers, versionChangeLabel } from './anomalies';

const DAY = 86_400_000;

function anom(over: Partial<Anomaly>): Anomaly {
  return {
    metric: 'cost',
    scope: 'workflow',
    workflow: 'a',
    bucket_ms: 0,
    observed: 2,
    expected: 1,
    deviation: 1,
    score: 5,
    direction: 'spike',
    severity: 'warning',
    summary: '',
    version_change: null,
    ...over,
  };
}

describe('anomalyValue', () => {
  it('formats each metric in its own units', () => {
    expect(anomalyValue('cost', 4.12)).toBe('$4.12');
    expect(anomalyValue('error_rate', 0.19)).toBe('19.0%');
    expect(anomalyValue('runs', 512)).toBe('512');
  });
});

describe('anomalyChips', () => {
  it('tallies by kind and drops empty kinds', () => {
    const chips = anomalyChips([
      anom({ metric: 'cost' }),
      anom({ metric: 'cost' }),
      anom({ metric: 'runs' }),
    ]);
    expect(chips.map((c) => [c.metric, c.label])).toEqual([
      ['cost', '2 cost spikes'],
      ['runs', '1 volume shift'],
    ]);
  });
});

describe('bySeverity', () => {
  it('orders by severity, then |score|, then recency', () => {
    const list = [
      anom({ severity: 'info', score: 3.2, bucket_ms: 10 }),
      anom({ severity: 'critical', score: 6.1, bucket_ms: 10 }),
      anom({ severity: 'critical', score: -9, bucket_ms: 10 }),
      anom({ severity: 'critical', score: 6.1, bucket_ms: 20 }),
      anom({ severity: 'warning', score: 5, bucket_ms: 10 }),
    ];
    const sorted = [...list].sort(bySeverity);
    expect(sorted.map((a) => [a.severity, a.score, a.bucket_ms])).toEqual([
      ['critical', -9, 10], // largest |score|
      ['critical', 6.1, 20], // more recent of the ties
      ['critical', 6.1, 10],
      ['warning', 5, 10],
      ['info', 3.2, 10],
    ]);
  });
});

describe('ratioLabel', () => {
  it('renders a clean multiple of the baseline', () => {
    expect(ratioLabel(4.6, 1)).toBe('4.6×');
    expect(ratioLabel(230, 10)).toBe('23×'); // large ratios drop the decimal
  });
  it('returns empty when no clean multiple exists', () => {
    expect(ratioLabel(5, 0)).toBe(''); // zero baseline
    expect(ratioLabel(0, 5)).toBe(''); // zero observed
  });
});

describe('groupIncidents', () => {
  it('clusters flags by target and day, worst severity wins the group', () => {
    const cost = anom({ metric: 'cost', workflow: 'checkout', bucket_ms: DAY, severity: 'critical', score: 6.4 });
    const err = anom({ metric: 'error_rate', workflow: 'checkout', bucket_ms: DAY, severity: 'warning', score: 5.2 });
    const other = anom({ metric: 'runs', scope: 'workspace', workflow: '', bucket_ms: 2 * DAY, severity: 'info', score: 3.2 });

    const incidents = groupIncidents([err, other, cost]);
    expect(incidents).toHaveLength(2);

    // Most severe incident first; its two flags collapse into one group.
    expect(incidents[0].workflow).toBe('checkout');
    expect(incidents[0].severity).toBe('critical');
    expect(incidents[0].anomalies).toHaveLength(2);
    // Within the group, most-actionable flag is first.
    expect(incidents[0].anomalies[0]).toBe(cost);

    expect(incidents[1].scope).toBe('workspace');
    expect(incidents[1].anomalies).toHaveLength(1);
  });

  it('keeps same-workflow different-day flags in separate incidents', () => {
    const day1 = anom({ workflow: 'a', bucket_ms: DAY });
    const day2 = anom({ workflow: 'a', bucket_ms: 2 * DAY });
    expect(groupIncidents([day1, day2])).toHaveLength(2);
  });
});

describe('toChartMarkers', () => {
  const axis = [0, DAY, 2 * DAY, 3 * DAY];

  it('aligns anomalies onto bucket indices and colours by severity', () => {
    const markers = toChartMarkers(
      [anom({ bucket_ms: 2 * DAY, severity: 'critical' })],
      axis,
      { label: (a) => anomalyValue(a.metric, a.observed) },
    );
    expect(markers).toHaveLength(1);
    expect(markers[0].index).toBe(2);
    expect(markers[0].color).toBe(SEVERITY_META.critical.color);
    expect(markers[0].label).toBe('$2.00');
  });

  it('drops anomalies whose bucket is outside the chart window', () => {
    const markers = toChartMarkers([anom({ bucket_ms: 99 * DAY })], axis);
    expect(markers).toHaveLength(0);
  });

  it('wires onSelect through to onClick', () => {
    let picked: Anomaly | null = null;
    const a = anom({ bucket_ms: DAY });
    const markers = toChartMarkers([a], axis, { onSelect: (x) => (picked = x) });
    markers[0].onClick?.();
    expect(picked).toBe(a);
  });
});

describe('versionChangeLabel', () => {
  const v = { workflow: 'rewrite-message', version: 'v2', first_seen_ms: 0 };
  it('names only the version on its own workflow, and the workflow too workspace-wide', () => {
    expect(versionChangeLabel(anom({}))).toBe('');
    expect(versionChangeLabel(anom({ version_change: v }))).toBe('after v2');
    expect(versionChangeLabel(anom({ scope: 'workspace', workflow: '', version_change: v }))).toBe('after rewrite-message v2');
  });
});
