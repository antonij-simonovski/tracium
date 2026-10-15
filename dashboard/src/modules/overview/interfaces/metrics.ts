import type { DeltaTone, Severity } from '../../../common';
import type { SetupIssue } from '../../../common/interfaces';

export interface Kpi {
  value: number;
  delta: number; // fraction: 0.12 === +12%
  delta_type: DeltaTone;
}

export interface KpiSet {
  cost: Kpi; // total cost, USD
  runs: Kpi; // workflow runs (distinct traces)
  latency_p95: Kpi; // p95 trace duration, ms
  error_rate: Kpi; // fraction of traces with an error
}

export interface CostBucket {
  bucket_ms: number;
  value: number;
}

export interface LatencyBucket {
  bucket_ms: number;
  // null for buckets with no runs — latency is undefined, not 0 (see LatencyPoint)
  p50: number | null;
  p95: number | null;
  p99: number | null; // all ms
}

export interface TokenBucket {
  bucket_ms: number;
  input_tokens: number;
  output_tokens: number;
}

export interface ErrorBucket {
  bucket_ms: number;
  errors: number; // runs that errored within the bucket
  total: number; // total runs that started within the bucket
}

export interface WorkflowCost {
  name: string;
  cost: number;
  calls: number;
  trend: number[]; // call count per time bucket, oldest first — the usage sparkline
}

export interface FailureRow {
  workflow: string;
  count: number;
  pct: number; // fraction of that workflow's runs that failed
}

// Anomaly is one bucket of one series that deviated significantly from its own
// recent history — the /metrics/anomalies payload. Detection is daily and
// rollup-backed; metric is the series that moved, scope/workflow locate it, and
// observed/expected/deviation/score carry the math the UI explains.
export type AnomalyMetric = 'cost' | 'error_rate' | 'runs';
export type AnomalyScope = 'workspace' | 'workflow';
export type AnomalyDirection = 'spike' | 'drop';
export type AnomalySeverity = Severity;

export interface Anomaly {
  metric: AnomalyMetric;
  scope: AnomalyScope;
  workflow: string; // set when scope === 'workflow', else ''
  bucket_ms: number; // anomalous day bucket, UTC midnight
  observed: number; // the bucket's value (USD, error rate 0–1, or run count)
  expected: number; // baseline median
  deviation: number; // observed − expected
  score: number; // signed robust z-score
  direction: AnomalyDirection;
  severity: AnomalySeverity;
  summary: string; // ready-to-read description
}

export interface SetupCheck extends SetupIssue {
  spans: number;
  share: number; // fraction of the window's spans
  example_trace_id: string; // empty for rejected spans, which are never stored
}
