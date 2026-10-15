import { useState } from 'react';
import {
  Centered,
  Spinner,
  fmtDelta,
  fmtNum,
  toCostPoints,
  toLatencyPoints,
  toErrorPoints,
  isLongRange,
} from '../../../common';
import { toRunRow } from '../../trace-explorer';
import {
  useWorkflowDetail,
  useWorkflowCostSeries,
  useWorkflowLatencySeries,
  useWorkflowErrorSeries,
  useWorkflowRuns,
  useWorkflowVersions,
} from '../hooks/useWorkflowDetail';
import { change, comparable, currentVersion, defaultPair, deriveRunOutcomes, fmtSeen, previousVersion, releaseMarkers } from '../utils';
import { WorkflowDetailPage } from './WorkflowDetailPage';
import type { MetaRowProps } from '../../../common';

// Cost per run this far above the previous version's raises the banner.
const COST_ALERT = 0.1;

interface WorkflowDetailLivePageProps {
  workflowName: string;
  range: string;
  setView: (v: string) => void;
  setSelected: (updater: (prev: Record<string, string>) => Record<string, string>) => void;
}

export function WorkflowDetailLivePage({ workflowName, range, setView, setSelected }: WorkflowDetailLivePageProps) {
  // Long ranges read the daily rollup, which has no latency.
  const raw = !isLongRange(range);
  const [filter, setFilter] = useState<string | null>(null);
  const versionsQuery = useWorkflowVersions(workflowName, range);
  const versions = versionsQuery.data?.items ?? [];
  const filtered = filter ? versions.find((v) => v.version === filter) : undefined;
  const version = filtered?.version;

  const detail = useWorkflowDetail(workflowName, range);
  const cost = useWorkflowCostSeries(workflowName, range, true, version);
  const latency = useWorkflowLatencySeries(workflowName, range, raw, version);
  const errors = useWorkflowErrorSeries(workflowName, range, true, version);
  const runs = useWorkflowRuns(workflowName, range, true, version);

  if (detail.isLoading) {
    return (
      <Centered>
        <Spinner />
      </Centered>
    );
  }
  if (!detail.data) {
    return <Centered>Failed to load workflow</Centered>;
  }

  const d = detail.data;
  const versioned = versions.some(comparable);
  const calls = filtered ? filtered.calls : d.calls;
  const errorRate = filtered ? filtered.error_rate : d.error_rate;
  const { completed, failed } = deriveRunOutcomes(calls, errorRate);

  const compare = (base: string, candidate: string) => {
    setSelected((s) => ({ ...s, workflow: workflowName, base, candidate }));
    setView('compare');
  };

  const current = currentVersion(versions);
  const pair = defaultPair(versions);
  const replaced = current && previousVersion(versions, current);
  const cprChange = current && replaced ? change(current.cost_per_run, replaced.cost_per_run) : 0;

  const bucketsMs = cost.data?.items.map((b) => b.bucket_ms) ?? [];
  const markers = releaseMarkers(versions, bucketsMs, undefined, (v) => {
    const prev = previousVersion(versions, v);
    if (prev) compare(prev.version, v.version);
  });

  const configRows: MetaRowProps[] = [
    { label: 'model', value: d.model || '—', mono: true },
    ...(d.provider ? [{ label: 'provider', value: d.provider }] : []),
    ...(current ? [{ label: 'version', value: current.version, mono: true, accent: true }] : []),
    { label: 'input tokens', value: fmtNum(d.input_tokens), mono: true },
    { label: 'output tokens', value: fmtNum(d.output_tokens), mono: true },
  ];

  return (
    <WorkflowDetailPage
      name={d.name}
      version={current?.version}
      versionSince={current ? fmtSeen(current.first_seen_ms) : undefined}
      range={range}
      scope={filtered ? `${filtered.version} only` : versioned ? 'all versions' : undefined}
      calls={calls}
      completed={completed}
      failed={failed}
      cost={filtered ? filtered.cost : d.cost}
      p95Ms={filtered ? filtered.p95_latency_ms : d.p95_latency_ms}
      errorRate={errorRate}
      configRows={configRows}
      costSeries={cost.data ? toCostPoints(cost.data.items, range) : []}
      latencySeries={latency.data ? toLatencyPoints(latency.data.items, range) : []}
      errorSeries={errors.data ? toErrorPoints(errors.data.items, range) : []}
      runs={runs.data ? runs.data.items.map(toRunRow) : []}
      markers={markers}
      versions={versioned ? versions : undefined}
      versionFilter={version ?? null}
      onVersionFilter={setFilter}
      onCompare={compare}
      defaultCompare={pair}
      costAlert={
        current && replaced && cprChange > COST_ALERT
          ? { version: current.version, from: replaced.cost_per_run, to: current.cost_per_run, change: fmtDelta(cprChange) }
          : undefined
      }
      loadErrors={{
        cost: cost.isError && !cost.data,
        latency: latency.isError && !latency.data,
        errors: errors.isError && !errors.data,
        runs: runs.isError && !runs.data,
      }}
      setView={setView}
      setSelected={setSelected}
    />
  );
}
