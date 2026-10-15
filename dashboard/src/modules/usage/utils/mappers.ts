import type { CostBucket, ErrorBucket, TokenBucket } from '../../overview/interfaces';
import type {
  DailySeriesPoint,
  TokenSeriesPoint,
  ModelCost,
  ModelSummary,
  WorkflowUsage,
  WorkflowSummary,
} from '../interfaces';

export const UNATTRIBUTED = '—';

// Bar colors for the models list, cycled in rank order (highest spend first).
const MODEL_COLORS = ['var(--accent)', '#7aa5ff', '#c08aff', '#f5a524', '#6366f1', '#34d399'];

function bucketLabel(ms: number, range: string): string {
  const d = new Date(ms);
  if (range === '24h') return `${String(d.getHours()).padStart(2, '0')}:00`;
  return `${d.getUTCMonth() + 1}/${d.getUTCDate()}`;
}

// Runs per bucket come from the error series' totals.
export function toDailySeries(cost: CostBucket[], errors: ErrorBucket[], range: string): DailySeriesPoint[] {
  const runs = new Map(errors.map((e) => [e.bucket_ms, e.total]));
  return cost.map((c) => ({
    label: bucketLabel(c.bucket_ms, range),
    cost: c.value,
    runs: runs.get(c.bucket_ms) ?? 0,
  }));
}

export function toTokenSeries(tokens: TokenBucket[], errors: ErrorBucket[], range: string): TokenSeriesPoint[] {
  const runs = new Map(errors.map((e) => [e.bucket_ms, e.total]));
  return tokens.map((t) => ({
    label: bucketLabel(t.bucket_ms, range),
    input: t.input_tokens,
    output: t.output_tokens,
    runs: runs.get(t.bucket_ms) ?? 0,
  }));
}

export const toModelSummaries = (models: ModelCost[]): ModelSummary[] =>
  models.map((m, i) => ({
    name: m.name,
    cost: m.cost,
    runs: m.calls,
    inputTokens: m.input_tokens,
    outputTokens: m.output_tokens,
    color: MODEL_COLORS[i % MODEL_COLORS.length],
  }));

export const toWorkflowSummaries = (workflows: WorkflowUsage[]): WorkflowSummary[] =>
  workflows.map((a) => ({
    name: a.name,
    model: a.model,
    cost: a.cost,
    costPrev: a.cost_prev,
    runs: a.runs,
    runsPrev: a.runs_prev,
    avg: a.runs > 0 ? a.cost / a.runs : 0,
  }));

export const sumCost = (rows: { cost: number }[]): number => rows.reduce((s, r) => s + r.cost, 0);
