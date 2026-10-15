import { BaseAPIClient } from './client';
import type { PaginatedResponse } from '../interfaces';
import type {
  KpiSet,
  CostBucket,
  LatencyBucket,
  ErrorBucket,
  TokenBucket,
  WorkflowCost,
  FailureRow,
  Anomaly,
  AnomalyMetric,
  AnomalySeverity,
  SetupCheck,
} from '../../modules/overview/interfaces';
import type {
  ModelCost,
  UserUsage,
  WorkflowUsage,
  AttributeUsage,
} from '../../modules/usage/interfaces';
import type { Workflow, WorkflowDetail, WorkflowVersion } from '../../modules/workflows/interfaces';

// MetricsAPI reads the aggregated overview metrics. Each section is its own
// endpoint so the dashboard can load and refresh them independently.
export class MetricsAPI extends BaseAPIClient {
  getKpis(range: string, userId?: string): Promise<KpiSet> {
    return this.get('/metrics/kpis', { range, user_id: userId });
  }

  // The series accept an optional `workflow` to scope the chart to one workflow (the
  // detail page), and `version` to one of its versions (raw ranges only, ≤30d).
  // Omitted, they return the workspace-wide series as before.
  getCostSeries(range: string, workflow?: string, userId?: string, version?: string): Promise<PaginatedResponse<CostBucket>> {
    return this.get('/metrics/cost-series', { range, workflow, user_id: userId, version });
  }

  getLatencySeries(range: string, workflow?: string, version?: string): Promise<PaginatedResponse<LatencyBucket>> {
    return this.get('/metrics/latency-series', { range, workflow, version });
  }

  getErrorSeries(range: string, workflow?: string, userId?: string, version?: string): Promise<PaginatedResponse<ErrorBucket>> {
    return this.get('/metrics/error-series', { range, workflow, user_id: userId, version });
  }

  getTopWorkflows(range: string): Promise<PaginatedResponse<WorkflowCost>> {
    return this.get('/metrics/top-workflows', { range });
  }

  getWorkflows(range: string): Promise<PaginatedResponse<Workflow>> {
    return this.get('/metrics/workflows', { range });
  }

  // One workflow's detail payload. Raw-window only (≤30d): the server rejects
  // longer ranges, since per-workflow latency isn't in the daily rollup.
  getWorkflowDetail(name: string, range: string): Promise<WorkflowDetail> {
    return this.get(`/metrics/workflows/${encodeURIComponent(name)}`, { range });
  }

  getWorkflowVersions(name: string, range: string): Promise<PaginatedResponse<WorkflowVersion>> {
    return this.get(`/metrics/workflows/${encodeURIComponent(name)}/versions`, { range });
  }

  getFailures(range: string): Promise<PaginatedResponse<FailureRow>> {
    return this.get('/metrics/failures', { range });
  }

  // Statistical anomalies (cost / error-rate / run-volume) over the window,
  // workspace-wide and per busy workflow. Daily and rollup-backed, so the server
  // requires a range of 7d or longer (24h is rejected). Optional metric and
  // min_severity narrow the results.
  getAnomalies(
    range: string,
    opts?: { metric?: AnomalyMetric; min_severity?: AnomalySeverity },
  ): Promise<PaginatedResponse<Anomaly>> {
    return this.get('/metrics/anomalies', { range, metric: opts?.metric, min_severity: opts?.min_severity });
  }

  getSetupChecks(range: string): Promise<PaginatedResponse<SetupCheck>> {
    return this.get('/metrics/setup-checks', { range });
  }

  // Usage-page breakdowns. cost-series / kpis feed the rest of the page.
  getTokenSeries(range: string, userId?: string, workflow?: string, version?: string): Promise<PaginatedResponse<TokenBucket>> {
    return this.get('/metrics/token-series', { range, user_id: userId, workflow, version });
  }

  getModelCosts(range: string, userId?: string): Promise<PaginatedResponse<ModelCost>> {
    return this.get('/metrics/model-costs', { range, user_id: userId });
  }

  getUserUsage(range: string, userId?: string): Promise<PaginatedResponse<UserUsage>> {
    return this.get('/metrics/usage-users', { range, user_id: userId });
  }

  getWorkflowUsage(range: string, userId?: string): Promise<PaginatedResponse<WorkflowUsage>> {
    return this.get('/metrics/usage-workflows', { range, user_id: userId });
  }

  // Custom-attribute allocation. attribute-keys lists the dimensions present in
  // the window (team, user.id, environment, …); usage-by-attribute groups spend
  // across the values of the chosen key.
  getAttributeKeys(range: string): Promise<PaginatedResponse<string>> {
    return this.get('/metrics/attribute-keys', { range });
  }

  getUsageByAttribute(range: string, key: string): Promise<PaginatedResponse<AttributeUsage>> {
    return this.get('/metrics/usage-by-attribute', { range, key });
  }
}
