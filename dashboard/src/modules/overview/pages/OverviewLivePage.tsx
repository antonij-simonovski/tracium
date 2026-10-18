import { useState } from 'react';
import {
  EmptyState,
  KpiStrip,
  Section,
  deltaParts,
  fmtCost,
  fmtNum,
  fmtMs,
  fmtPct,
  isLongRange,
  rangeLabel,
  relativeTime,
  toCostPoints,
  toLatencyPoints,
  toErrorPoints,
} from '../../../common';
import type { KpiItem } from '../../../common';
import type { CostPoint, LatencyPoint, ErrorPoint } from '../../../common/interfaces';
import { useAPIClient } from '../../../common/providers/APIProvider';
import type { Trace } from '../../trace-explorer/interfaces';
import {
  Masthead,
  ChartsRow,
  FailuresBlock,
  TopWorkflows,
  ActivityFeed,
  OverviewLayout,
  OutlierChips,
  OutliersPanel,
  SetupChecks,
} from '../components';
import {
  useKpis,
  useCostSeries,
  useLatencySeries,
  useErrorSeries,
  useTopWorkflows,
  useFailures,
  useAnomalies,
  useSetupChecks,
  useRecentActivity,
} from '../hooks/useMetrics';
import type { KpiSet, ActivityItem, Anomaly } from '../interfaces';
import { anomalyKey, anomalyValue, toChartMarkers } from '../utils/anomalies';
import { failuresSummary, syncStatus } from '../utils/status';

interface OverviewLivePageProps {
  range: string;
  setView: (v: string) => void;
  setSelected: (updater: (prev: Record<string, string>) => Record<string, string>) => void;
}

function toKpiItems(kpis: KpiSet, cost: CostPoint[], latency: LatencyPoint[], errors: ErrorPoint[], range: string): KpiItem[] {
  // An empty bucket reads as a 0% failure rate: no runs means nothing failed.
  const tracesTrend = errors.map((d) => d.total);
  const failureTrend = errors.map((d) => (d.total > 0 ? d.errors / d.total : 0));
  const latencyItem: KpiItem = !isLongRange(range)
    ? { label: 'p95 latency', value: fmtMs(kpis.latency_p95.value), ...deltaParts(kpis.latency_p95), sparkData: latency.map((d) => d.p95), sparkColor: 'var(--accent)' }
    : { label: 'p95 latency', value: '—', hint: 'not tracked over long ranges' };
  return [
    { label: 'Total spend', value: fmtCost(kpis.cost.value), ...deltaParts(kpis.cost), sparkData: cost.map((d) => d.value) },
    { label: 'Traces', value: fmtNum(Math.round(kpis.runs.value)), ...deltaParts(kpis.runs), sparkData: tracesTrend },
    { label: 'Failure rate', value: fmtPct(kpis.error_rate.value * 100), ...deltaParts(kpis.error_rate), sparkData: failureTrend, sparkColor: 'var(--warning)' },
    latencyItem,
  ];
}

const toActivityItems = (traces: Trace[]): ActivityItem[] =>
  traces.map((t) => ({
    id: t.trace_id,
    workflow: t.name,
    status: t.has_error ? 'failed' : 'completed',
    time: relativeTime(t.start_time_ms),
    latency: t.duration_ms / 1000,
  }));

export function OverviewLivePage({ range, setView, setSelected }: OverviewLivePageProps) {
  const kpis = useKpis(range);
  const cost = useCostSeries(range);
  const latency = useLatencySeries(range);
  const workflows = useTopWorkflows(range);
  const failures = useFailures(range);
  const errorSeries = useErrorSeries(range);
  const anomalies = useAnomalies(range);
  const setupChecks = useSetupChecks();
  const { workspaceId } = useAPIClient();
  const activity = useRecentActivity();

  // Dismissal is session-local; there is no dismiss endpoint yet.
  const [dismissed, setDismissed] = useState<Set<string>>(new Set());
  const [selectedOutlier, setSelectedOutlier] = useState<string | null>(null);
  // Also shown on the empty state: rejected spans leave a workspace looking empty.
  const setupChecksBlock = workspaceId && setupChecks.data && setupChecks.data.items.length > 0 && (
    <SetupChecks
      key={workspaceId}
      checks={setupChecks.data.items}
      workspaceId={workspaceId}
      onOpenTrace={(id) => {
        setSelected((s) => ({ ...s, traceId: id }));
        setView('trace');
      }}
    />
  );

  // The activity feed has no time bound, so it tells a never-ingested
  // workspace apart from an empty range. Cost can come from metrics without
  // spans, so a workspace with spend but no runs still gets the dashboard.
  if (
    kpis.isSuccess &&
    kpis.data.runs.value === 0 &&
    kpis.data.cost.value === 0 &&
    activity.isSuccess
  ) {
    const hasAnyTraces = activity.data.items.length > 0;
    return (
      <div>
        {setupChecksBlock && <div style={{ maxWidth: 880, margin: '0 auto', padding: '32px 16px 0' }}>{setupChecksBlock}</div>}
        {hasAnyTraces ? (
          <EmptyState
            message={`No traces in the ${rangeLabel(range)}`}
            description="Your workflows haven't reported any activity in this window. Try a wider time range to see earlier traces."
          />
        ) : (
          <>
            <EmptyState
              message="Connect your application to see data"
              description="Create an API key for this workspace, add it to your OpenTelemetry exporter, then send your first trace."
            />
            <div style={{ display: 'flex', justifyContent: 'center', padding: '0 24px 40px' }}>
              <button onClick={() => setView('settings')} style={{ padding: '9px 16px', borderRadius: 7, border: '1px solid var(--accent)', background: 'var(--accent)', color: 'var(--accent-contrast)', fontFamily: 'inherit', fontSize: 14, fontWeight: 600, cursor: 'pointer' }}>View setup instructions</button>
            </div>
          </>
        )}
      </div>
    );
  }

  const status = syncStatus([kpis, cost, latency, workflows, failures, errorSeries, activity]);

  const costPoints = cost.data ? toCostPoints(cost.data.items, range) : [];
  const latencyPoints = latency.data ? toLatencyPoints(latency.data.items, range) : [];
  const summary = failures.data ? failuresSummary(failures.data.items, failures.data.total) : null;
  const errorPoints = errorSeries.data ? toErrorPoints(errorSeries.data.items, range) : [];

  // Detection is daily, so there are no outliers at 24h.
  const outliersAvailable = range !== '24h';
  const allAnoms = anomalies.data?.items ?? [];
  const liveAnoms = allAnoms.filter((a) => !dismissed.has(anomalyKey(a)));
  const dismissedCount = allAnoms.length - liveAnoms.length;
  const costAxisMs = cost.data ? cost.data.items.map((b) => b.bucket_ms) : [];
  const errorAxisMs = errorSeries.data ? errorSeries.data.items.map((b) => b.bucket_ms) : [];
  const selectOutlier = (a: Anomaly) => setSelectedOutlier(anomalyKey(a));
  const costMarkers = toChartMarkers(
    liveAnoms.filter((a) => a.metric === 'cost'),
    costAxisMs,
    { label: (a) => anomalyValue(a.metric, a.observed), onSelect: selectOutlier },
  );
  // Run-volume shifts aren't failures, so only error-rate flags go on the strip.
  const errorMarkers = toChartMarkers(
    liveAnoms.filter((a) => a.metric === 'error_rate'),
    errorAxisMs,
    { onSelect: selectOutlier },
  );
  const dismissOutlier = (a: Anomaly) => setDismissed((s) => new Set(s).add(anomalyKey(a)));
  const inspectOutlier = (a: Anomaly) => {
    if (a.workflow) setSelected((s) => ({ ...s, workflow: a.workflow }));
    setView('workflows');
  };

  return (
    <OverviewLayout
      masthead={
        <Masthead
          eyebrow="Workspace"
          title="Overview"
          subtitle={`Workspace health and AI spend across the ${rangeLabel(range)}.`}
          status={status}
        />
      }
      setupChecks={setupChecksBlock}
      kpis={
        <Section isLoading={kpis.isLoading} isError={kpis.isError}>
          {kpis.data && <KpiStrip items={toKpiItems(kpis.data, costPoints, latencyPoints, errorPoints, range)} />}
        </Section>
      }
      outlierChips={
        outliersAvailable && anomalies.data ? <OutlierChips anomalies={liveAnoms} /> : undefined
      }
      charts={
        <Section
          isLoading={cost.isLoading || latency.isLoading}
          isError={cost.isError || latency.isError}
          minHeight={240}
        >
          <ChartsRow costSeries={costPoints} latSeries={latencyPoints} range={range} costMarkers={costMarkers} />
        </Section>
      }
      outliers={
        outliersAvailable ? (
          <Section isLoading={anomalies.isLoading} isError={anomalies.isError}>
            {anomalies.data && (
              <OutliersPanel
                anomalies={liveAnoms}
                range={range}
                dismissedCount={dismissedCount}
                selectedKey={selectedOutlier}
                onSelectKey={setSelectedOutlier}
                onDismiss={dismissOutlier}
                onRestoreAll={() => setDismissed(new Set())}
                onInspect={inspectOutlier}
              />
            )}
          </Section>
        ) : undefined
      }
      failures={
        <Section
          isLoading={failures.isLoading || errorSeries.isLoading}
          isError={failures.isError || errorSeries.isError}
        >
          {summary && (
            <FailuresBlock
              series={errorPoints}
              totalFailed={summary.totalFailed}
              worstWorkflow={summary.worstWorkflow}
              onViewWorkflows={() => setView('workflows')}
              range={range}
              errorMarkers={errorMarkers}
            />
          )}
        </Section>
      }
      feed={
        <Section isLoading={activity.isLoading} isError={activity.isError}>
          {activity.data && (
            <ActivityFeed
              items={toActivityItems(activity.data.items)}
              onSelectTrace={(id) => {
                setSelected((s) => ({ ...s, traceId: id }));
                setView('trace');
              }}
            />
          )}
        </Section>
      }
      workflows={
        <Section isLoading={workflows.isLoading} isError={workflows.isError}>
          {workflows.data && (
            <TopWorkflows
              range={range}
              workflows={workflows.data.items}
              onSelectWorkflow={(name) => {
                setSelected((s) => ({ ...s, workflow: name }));
                setView('workflows');
              }}
            />
          )}
        </Section>
      }
    />
  );
}
