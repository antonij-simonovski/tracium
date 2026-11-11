// API response shape for GET /v1/metrics/workflows/{name}/versions. version is
// '' for the unversioned runs; p95_latency_ms is null over ranges longer than 30d.
export interface WorkflowVersion {
  version: string;
  first_seen_ms: number;
  last_seen_ms: number;
  calls: number;
  cost: number;
  cost_per_run: number;
  p95_latency_ms: number | null;
  error_rate: number;
  tokens_per_run: number;
  // Absent when the version has no release report.
  report_verdict?: ReleaseVerdict;
}

export type ReleaseVerdict = 'pending' | 'ok' | 'improved' | 'regressed' | 'insufficient';

export interface ReleaseFinding {
  metric: string;
  prev: number;
  next: number;
  verdict: 'regressed' | 'improved' | 'same';
  severity: '' | 'info' | 'warning' | 'critical';
}

// API response shape for GET /v1/metrics/workflows/{name}/versions/{version}/report.
export interface ReleaseReport {
  workspace_id: string;
  workflow: string;
  version: string;
  prev_version: string;
  first_seen_at: string;
  status: 'pending' | 'final';
  verdict: '' | Exclude<ReleaseVerdict, 'pending'>;
  runs: number;
  prev_runs: number;
  findings: ReleaseFinding[];
  created_at: string;
  finalized_at: string | null;
}
