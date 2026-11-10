import { SEVERITY_META, relativeTime, type Severity, type SilentFailure } from '../../../common';
import type { AnomalyMetric } from '../../overview/interfaces';
import { METRIC_META } from '../../overview/utils/anomalies';
import type { Incident, IncidentSignal } from '../interfaces';

export const METRIC_FLAG: Partial<Record<AnomalyMetric, SilentFailure>> = {
  truncation_rate: 'truncated',
  refusal_rate: 'refused',
  tool_error_rate: 'tool_error',
};

export function bySeverity(a: Incident, b: Incident): number {
  return SEVERITY_META[b.severity].rank - SEVERITY_META[a.severity].rank || Date.parse(b.opened_at) - Date.parse(a.opened_at);
}

export function subjectLabel(inc: Pick<Incident, 'scope' | 'subject'>): string {
  if (inc.scope === 'workflow') return inc.subject;
  if (inc.scope === 'client') return `Client ${inc.subject}`;
  if (inc.scope === 'service') return `Service ${inc.subject}`;
  return 'Workspace-wide';
}

export const sinceLabel = (iso: string) => relativeTime(Date.parse(iso));

export function signalsOf(inc: Incident): IncidentSignal[] {
  return inc.signals ?? [];
}

export function sampleTraceIds(inc: Incident): string[] {
  return [...new Set(signalsOf(inc).flatMap((s) => s.trace_ids ?? []))];
}

export function groupByMetric(signals: IncidentSignal[]): [AnomalyMetric, IncidentSignal[]][] {
  const groups = new Map<AnomalyMetric, IncidentSignal[]>();
  for (const s of [...signals].sort((a, b) => b.bucket_ms - a.bucket_ms)) {
    (groups.get(s.metric) ?? groups.set(s.metric, []).get(s.metric)!).push(s);
  }
  return [...groups];
}

// The flag of the incident's first silent-failure signal, for the pre-filtered run list.
export function incidentFlag(inc: Incident): SilentFailure | undefined {
  return signalsOf(inc).map((s) => METRIC_FLAG[s.metric]).find(Boolean);
}

// Hourly signals are at most a few hours old, daily ones a day or two.
export function incidentRange(inc: Incident): '24h' | '7d' {
  return signalsOf(inc).every((s) => METRIC_META[s.metric]?.hourly) ? '24h' : '7d';
}

// Worst open severity per workflow; workflows without an open incident are absent.
export function workflowStatus(open: Incident[]): Record<string, Severity> {
  const out: Record<string, Severity> = {};
  for (const inc of open) {
    if (inc.scope !== 'workflow' || inc.severity === 'info') continue;
    const prev = out[inc.subject];
    if (!prev || SEVERITY_META[inc.severity].rank > SEVERITY_META[prev].rank) out[inc.subject] = inc.severity;
  }
  return out;
}

export function openIncidentFor(open: Incident[], scope: string, subject: string): Incident | undefined {
  return open.find((inc) => inc.scope === scope && inc.subject === subject);
}
