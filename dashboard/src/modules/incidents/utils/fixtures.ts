import type { Incident } from '../interfaces';

export const INCIDENT: Incident = {
  id: 'inc-1', workspace_id: 'ws-1', scope: 'workflow', subject: 'sf-truncation', status: 'open', severity: 'critical',
  title: 'sf-truncation: truncated LLM calls at 66.7%', opened_at: new Date(Date.now() - 2 * 3_600_000).toISOString(),
  updated_at: '', resolved_at: null, resolved_by: '', last_signal_at: '',
  version_change: { workflow: 'sf-truncation', version: 'v42', first_seen_ms: 0 },
  signals: [{ metric: 'truncation_rate', bucket_ms: 0, observed: 0.667, expected: 0.01, score: 66, direction: 'spike', severity: 'critical', summary: 'Truncated calls rose', trace_ids: ['t1'] }],
};
