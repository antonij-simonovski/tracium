import { bucketLabel, fmtCost, fmtMs, fmtNum, fmtPct, SEVERITY_META } from '../../../common';
import type { ChartMarker } from '../../../common/interfaces';
import type { Anomaly, AnomalyMetric, AnomalyScope, AnomalySeverity } from '../interfaces';

export const METRIC_META: Record<AnomalyMetric, { noun: string; kind: string; hourly?: boolean }> = {
  cost: { noun: 'Cost', kind: 'Cost' },
  error_rate: { noun: 'Error rate', kind: 'Errors' },
  runs: { noun: 'Run volume', kind: 'Volume' },
  latency_p95: { noun: 'p95 latency', kind: 'Latency' },
  truncation_rate: { noun: 'Truncated calls', kind: 'Truncation', hourly: true },
  refusal_rate: { noun: 'Refused calls', kind: 'Refusals', hourly: true },
  tool_error_rate: { noun: 'Tool errors', kind: 'Tool errors', hourly: true },
  llm_calls_per_run: { noun: 'LLM calls per run', kind: 'LLM calls', hourly: true },
  tool_repeat: { noun: 'Repeated tool calls', kind: 'Tool loop', hourly: true },
  cache_hit_ratio: { noun: 'Cache hit ratio', kind: 'Cache hits', hourly: true },
  heartbeat: { noun: 'Hours silent', kind: 'Heartbeat', hourly: true },
};

export function anomalyValue(metric: AnomalyMetric, v: number): string {
  switch (metric) {
    case 'cost':
      return fmtCost(v);
    case 'error_rate':
    case 'truncation_rate':
    case 'refusal_rate':
    case 'tool_error_rate':
    case 'cache_hit_ratio':
      return fmtPct(v * 100);
    case 'latency_p95':
      return fmtMs(v);
    case 'heartbeat':
      return `${fmtNum(Math.round(v))}h`;
    default:
      return fmtNum(Math.round(v));
  }
}

export function zLabel(score: number): string {
  return `${Math.abs(score).toFixed(1)}σ`;
}

// Hourly metrics score a multiple of their baseline, not a z-score.
export function scoreLabel(a: Pick<Anomaly, 'metric' | 'score'>): string {
  return METRIC_META[a.metric].hourly ? `${Math.abs(a.score).toFixed(1)}×` : zLabel(a.score);
}

export function anomalyTarget(a: { scope: AnomalyScope; workflow: string; client: string }): string {
  if (a.scope === 'workflow') return a.workflow;
  if (a.scope === 'client') return `Client ${a.client}`;
  return 'Workspace-wide';
}

export function anomalyTime(a: Pick<Anomaly, 'metric' | 'bucket_ms'>, range: string): string {
  if (!METRIC_META[a.metric].hourly) return bucketLabel(a.bucket_ms, range);
  const d = new Date(a.bucket_ms);
  return `${d.getUTCMonth() + 1}/${d.getUTCDate()} ${String(d.getUTCHours()).padStart(2, '0')}:00 UTC`;
}

// Workflow-scoped anomalies already name the workflow, so only the version is shown.
export function versionChangeLabel(a: Anomaly): string {
  const v = a.version_change;
  if (!v) return '';
  return a.scope === 'workflow' ? `after ${v.version}` : `after ${v.workflow} ${v.version}`;
}

// The model carries no id; this one drives selection and dismissal.
export function anomalyKey(a: Anomaly): string {
  return `${a.metric}:${a.scope}:${a.workflow}:${a.client}:${a.bucket_ms}`;
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
  { metric: 'latency_p95', noun: 'latency spike', sev: 'warning' },
  { metric: 'truncation_rate', noun: 'truncation spike', sev: 'critical' },
  { metric: 'refusal_rate', noun: 'refusal spike', sev: 'critical' },
  { metric: 'tool_error_rate', noun: 'hidden tool error spike', sev: 'critical' },
  { metric: 'llm_calls_per_run', noun: 'runaway run', sev: 'critical' },
  { metric: 'tool_repeat', noun: 'tool loop', sev: 'critical' },
  { metric: 'cache_hit_ratio', noun: 'cache drop', sev: 'warning' },
  { metric: 'heartbeat', noun: 'silent client', sev: 'warning' },
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
  client: string;
  bucket_ms: number;
  severity: AnomalySeverity;
  anomalies: Anomaly[];
}

export function groupIncidents(anomalies: Anomaly[]): Incident[] {
  const byKey = new Map<string, Incident>();
  for (const a of anomalies) {
    const key = `${a.scope}:${a.workflow}:${a.client}:${a.bucket_ms}`;
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
        client: a.client,
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
