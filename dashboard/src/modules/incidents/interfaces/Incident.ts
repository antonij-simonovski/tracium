import type { Severity } from '../../../common';
import type { AnomalyMetric, VersionChange } from '../../overview/interfaces';

export type IncidentStatus = 'open' | 'resolved';
export type IncidentScope = 'workspace' | 'workflow' | 'client' | 'service';
export type MuteDuration = '24h' | '7d' | 'forever';

export interface IncidentSignal {
  metric: AnomalyMetric;
  bucket_ms: number;
  observed: number;
  expected: number;
  score: number;
  direction: 'spike' | 'drop';
  severity: Severity;
  summary: string;
  trace_ids: string[] | null;
}

export interface Incident {
  id: string;
  workspace_id: string;
  scope: IncidentScope;
  subject: string;
  status: IncidentStatus;
  severity: Severity;
  title: string;
  opened_at: string;
  updated_at: string;
  resolved_at: string | null;
  resolved_by: string;
  last_signal_at: string;
  version_change: VersionChange | null;
  signals: IncidentSignal[] | null;
}

export interface MuteInput {
  duration: MuteDuration;
  metric: string;
}
