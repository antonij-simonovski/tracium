// Where a version came from: explicit (tracium.workflow.version), prompt (the
// managed prompt version), service (service.version / vcs revision), or '' when
// the runs carry none.
export type VersionSource = '' | 'explicit' | 'prompt' | 'service';

// API response shape for GET /v1/metrics/workflows/{name}/versions. version is
// '' for the unversioned runs; p95_latency_ms is null over ranges longer than 30d.
export interface WorkflowVersion {
  version: string;
  source: VersionSource;
  first_seen_ms: number;
  last_seen_ms: number;
  calls: number;
  cost: number;
  cost_per_run: number;
  p95_latency_ms: number | null;
  error_rate: number;
  tokens_per_run: number;
}
