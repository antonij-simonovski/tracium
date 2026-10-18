import { fmtNum } from '../../../common';
import type { MetaRowProps } from '../../../common';
import type { LatencyPoint } from '../../../common/interfaces';
import { WORKFLOW_META, DEFAULT_WORKFLOW_META } from '../data';
import { buildCostSeries, buildLatencySeries, buildErrorSeries, buildRuns, deriveRunOutcomes } from '../utils';
import { WorkflowDetailPage } from './WorkflowDetailPage';
import type { Workflow } from '../interfaces';

interface WorkflowDetailDemoPageProps {
  workflow: Workflow;
  range: string;
  setView: (v: string) => void;
  setSelected: (updater: (prev: Record<string, string>) => Record<string, string>) => void;
}

// The demo latency series is in seconds (see series.ts); the page wants p95 in ms.
function p95Ms(series: LatencyPoint[]): number | null {
  const vals = series.map((d) => d.p95 ?? 0);
  return vals.length ? Math.max(...vals) * 1000 : null;
}

export function WorkflowDetailDemoPage({ workflow, range, setView, setSelected }: WorkflowDetailDemoPageProps) {
  const meta = WORKFLOW_META[workflow.name] ?? DEFAULT_WORKFLOW_META;
  const costSeries = buildCostSeries(workflow, range);
  const latencySeries = buildLatencySeries(workflow, range);
  const errorSeries = buildErrorSeries(workflow, range);
  const runs = buildRuns(workflow);
  const { completed, failed } = deriveRunOutcomes(workflow.calls, workflow.error_rate);

  const configRows: MetaRowProps[] = [
    { label: 'endpoint', value: meta.endpoint, mono: true },
    { label: 'model', value: meta.model, mono: true },
    { label: 'provider', value: meta.provider },
    { label: 'version', value: meta.version, mono: true, accent: true },
    { label: 'temperature', value: meta.temperature.toFixed(1), mono: true },
    { label: 'max_tokens', value: fmtNum(meta.maxTokens), mono: true },
    { label: 'timeout', value: meta.timeout, mono: true },
    { label: 'retries', value: meta.retries, mono: true },
  ];

  return (
    <WorkflowDetailPage
      name={workflow.name}
      version={meta.version}
      description={meta.description}
      range={range}
      calls={workflow.calls}
      completed={completed}
      failed={failed}
      cost={workflow.cost}
      p95Ms={p95Ms(latencySeries)}
      errorRate={workflow.error_rate}
      configRows={configRows}
      costSeries={costSeries}
      latencySeries={latencySeries}
      errorSeries={errorSeries}
      runs={runs}
      setView={setView}
      setSelected={setSelected}
    />
  );
}
