import type { Kpi } from '../../overview/interfaces';
import type { WorkflowVersion } from './WorkflowVersion';

// API response shape for GET /v1/metrics/workflows/{name}/versions/compare. Each
// Kpi is the candidate's value and its change from the base.
export interface VersionComparison {
  base: WorkflowVersion;
  candidate: WorkflowVersion;
  cost_per_run: Kpi;
  p95_latency_ms: Kpi;
  error_rate: Kpi;
  tokens_per_run: Kpi;
}
