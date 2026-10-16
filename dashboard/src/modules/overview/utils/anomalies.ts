import { fmtCost, fmtNum, fmtPct, SEVERITY_META } from '../../../common';
import type { ChartMarker } from '../../../common/interfaces';
import type { Anomaly, AnomalyMetric, AnomalyScope, AnomalySeverity } from '../interfaces';

export const METRIC_META: Record<AnomalyMetric, { noun: string; kind: string }> = {
  cost: { noun: 'Cost', kind: 'Cost' },
  error_rate: { noun: 'Error rate', kind: 'Errors' },
  runs: { noun: 'Run volume', kind: 'Volume' },
};

export function anomalyValue(metric: AnomalyMetric, v: number): string {
  switch (metric) {
    case 'cost':
      return fmtCost(v);
    case 'error_rate':
      return fmtPct(v * 100);
    default:
      return fmtNum(Math.round(v));
  }
}

export function zLabel(score: number): string {
  return `${Math.abs(score).toFixed(1)}σ`;
}

// Workflow-scoped anomalies already name the workflow, so only the version is shown.
export function versionChangeLabel(a: Anomaly): string {
  const v = a.version_change;
  if (!v) return '';
  return a.scope === 'workflow' ? `after ${v.version}` : `after ${v.workflow} ${v.version}`;
}

// The model carries no id; this one drives selection and dismissal.
export function anomalyKey(a: Anomaly): string {
  return `${a.metric}:${a.scope}:${a.workflow}:${a.bucket_ms}`;
}

export interface AnomalyChip {
  metric: AnomalyMetric;
  label: string;
  color: string;
  tint: string;
  border: string;
}

const CHIP_DEFS: { metric: AnomalyMetric; noun: string; sev: AnomalySeverity }[] = [
  { metric: 'cost', noun: 'cost spike', sev: 'critical' },
  { metric: 'error_rate', noun: 'error spike', sev: 'critical' },
  { metric: 'runs', noun: 'volume shift', sev: 'warning' },
];

export function anomalyChips(anomalies: Anomaly[]): AnomalyChip[] {
  const counts = new Map<AnomalyMetric, number>();
  for (const a of anomalies) counts.set(a.metric, (counts.get(a.metric) ?? 0) + 1);
  return CHIP_DEFS.filter((d) => counts.has(d.metric)).map((d) => {
    const n = counts.get(d.metric)!;
    const { color, tint, border } = SEVERITY_META[d.sev];
    return { metric: d.metric, label: `${n} ${d.noun}${n === 1 ? '' : 's'}`, color, tint, border };
  });
}

// Same order the API returns: severity, then |score|, then most recent.
export function bySeverity(a: Anomaly, b: Anomaly): number {
  const s = SEVERITY_META[b.severity].rank - SEVERITY_META[a.severity].rank;
  if (s !== 0) return s;
  const z = Math.abs(b.score) - Math.abs(a.score);
  if (z !== 0) return z;
  return b.bucket_ms - a.bucket_ms;
}

// Empty when there is no clean multiple, so callers fall back to the deviation.
export function ratioLabel(observed: number, expected: number): string {
  if (expected <= 0 || observed <= 0) return '';
  const r = observed / expected;
  if (!Number.isFinite(r)) return '';
  return `${r >= 10 ? Math.round(r) : r.toFixed(1)}×`;
}

// Every anomaly for one target on one day; severity is the worst in the group.
export interface Incident {
  key: string;
  scope: AnomalyScope;
  workflow: string; // '' for workspace-wide
  bucket_ms: number;
  severity: AnomalySeverity;
  anomalies: Anomaly[];
}

export function groupIncidents(anomalies: Anomaly[]): Incident[] {
  const byKey = new Map<string, Incident>();
  for (const a of anomalies) {
    const key = `${a.scope}:${a.workflow}:${a.bucket_ms}`;
    const existing = byKey.get(key);
    if (existing) {
      existing.anomalies.push(a);
      if (SEVERITY_META[a.severity].rank > SEVERITY_META[existing.severity].rank) {
        existing.severity = a.severity;
      }
    } else {
      byKey.set(key, {
        key,
        scope: a.scope,
        workflow: a.workflow,
        bucket_ms: a.bucket_ms,
        severity: a.severity,
        anomalies: [a],
      });
    }
  }
  const incidents = [...byKey.values()];
  for (const inc of incidents) inc.anomalies.sort(bySeverity);
  incidents.sort((x, y) => {
    const s = SEVERITY_META[y.severity].rank - SEVERITY_META[x.severity].rank;
    if (s !== 0) return s;
    const z = Math.abs(y.anomalies[0].score) - Math.abs(x.anomalies[0].score);
    if (z !== 0) return z;
    return y.bucket_ms - x.bucket_ms;
  });
  return incidents;
}

// At most one marker per bucket and `max` overall, keeping the most severe;
// anomalies outside the chart window are dropped.
export function toChartMarkers(
  anomalies: Anomaly[],
  bucketMs: number[],
  opts?: { label?: (a: Anomaly) => string; onSelect?: (a: Anomaly) => void; max?: number },
): ChartMarker[] {
  const indexOf = new Map(bucketMs.map((ms, i) => [ms, i]));
  const max = opts?.max ?? 4;
  const seen = new Set<number>();
  const markers: ChartMarker[] = [];
  for (const a of [...anomalies].sort(bySeverity)) {
    const index = indexOf.get(a.bucket_ms);
    if (index === undefined || seen.has(index)) continue;
    seen.add(index);
    markers.push({
      index,
      color: SEVERITY_META[a.severity].color,
      label: opts?.label?.(a),
      onClick: opts?.onSelect ? () => opts.onSelect!(a) : undefined,
    });
    if (markers.length >= max) break;
  }
  return markers;
}
