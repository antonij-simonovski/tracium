import { useState } from 'react';
import {
  EmptyState,
  KpiStrip,
  LastUpdated,
  Panel,
  PanelFrame,
  RunsTable,
  Section,
  SectionHead,
  deltaParts,
  fmtMs,
  fmtNum,
  fmtPct,
  isLongRange,
  periodLabel,
  relativeTime,
  useMaxWidth,
  BREAKPOINTS,
} from '../../../common';
import type { KpiItem } from '../../../common';
import type { KpiSet } from '../../overview/interfaces';
import { toRunRow } from '../../trace-explorer';
import { DailyChart, DailyChartLegend, ModelsList, Breakdown } from '../components';
import type { SortKey } from '../components';
import {
  useClientKpis,
  useClientCostSeries,
  useClientErrorSeries,
  useClientModelCosts,
  useClientWorkflowUsage,
  useClientTraces,
} from '../hooks/useClientDetail';
import {
  toDailySeries,
  toModelSummaries,
  toWorkflowSummaries,
  sumCost,
  spendTile,
  runsTile,
  tokensTile,
} from '../utils';
import styles from './UserDetailLivePage.module.css';

interface Props {
  userId: string;
  range: string;
  setView: (view: string) => void;
  setSelected: (updater: (prev: Record<string, string>) => Record<string, string>) => void;
}

function toKpiItems(kpis: KpiSet, tokens: number | undefined, range: string): KpiItem[] {
  const failed = Math.round(kpis.runs.value * kpis.error_rate.value);
  return [
    spendTile(kpis),
    runsTile(kpis),
    {
      label: 'Failure rate',
      value: fmtPct(kpis.error_rate.value * 100),
      ...deltaParts(kpis.error_rate),
      hint: `${fmtNum(failed)} failed`,
    },
    isLongRange(range)
      ? tokensTile(tokens)
      : {
          label: 'p95 latency',
          value: fmtMs(kpis.latency_p95.value),
          ...deltaParts(kpis.latency_p95),
          hint: 'end-to-end',
        },
  ];
}

export function UserDetailLivePage({ userId, range, setView, setSelected }: Props) {
  const [sortBy, setSortBy] = useState<SortKey>('cost');
  const stacked = useMaxWidth(BREAKPOINTS.tablet);

  const kpis = useClientKpis(userId, range);
  const cost = useClientCostSeries(userId, range);
  const errors = useClientErrorSeries(userId, range);
  const models = useClientModelCosts(userId, range);
  const workflows = useClientWorkflowUsage(userId, range);
  const traces = useClientTraces(userId, range);

  const openTrace = (id: string) => {
    setSelected((prev) => ({ ...prev, traceId: id }));
    setView('trace');
  };
  const openWorkflow = (name: string) => {
    setSelected((prev) => ({ ...prev, workflow: name }));
    setView('workflows');
  };

  const back = (
    <button className={styles.back} onClick={() => setView('users')}>
      ← All clients
    </button>
  );

  if (!userId) {
    return (
      <div className={styles.page}>
        {back}
        <EmptyState message="No client selected" description="Pick a client from the Clients list." />
      </div>
    );
  }

  const noUsage = kpis.isSuccess && kpis.data.cost.value === 0 && kpis.data.runs.value === 0;

  const modelRows = models.data ? toModelSummaries(models.data.items) : [];
  const totalTokens = models.data
    ? modelRows.reduce((s, m) => s + m.inputTokens + m.outputTokens, 0)
    : undefined;
  const workflowRows = workflows.data ? toWorkflowSummaries(workflows.data.items) : [];
  const dailySeries =
    cost.data && errors.data ? toDailySeries(cost.data.items, errors.data.items, range) : [];
  const lastSeen = traces.data?.items[0]?.start_time_ms;

  const updatedAt =
    Math.max(
      kpis.dataUpdatedAt,
      cost.dataUpdatedAt,
      errors.dataUpdatedAt,
      models.dataUpdatedAt,
      workflows.dataUpdatedAt,
      traces.dataUpdatedAt,
    ) || undefined;

  return (
    <div className={styles.page}>
      {back}

      <header className={styles.masthead}>
        <div className={styles.text}>
          <h1 className={styles.title}>{userId}</h1>
          <p className={styles.subtitle}>
            {periodLabel(range)}
            {lastSeen != null && (
              <>
                <span className={styles.sep}>·</span>
                Last seen <span className={styles.strong}>{relativeTime(lastSeen)}</span>
              </>
            )}
            {workflowRows.length > 0 && (
              <>
                <span className={styles.sep}>·</span>
                {workflowRows.length} {workflowRows.length === 1 ? 'workflow' : 'workflows'}
              </>
            )}
          </p>
        </div>
        {updatedAt != null && <LastUpdated at={updatedAt} />}
      </header>

      <Section isLoading={kpis.isLoading} isError={kpis.isError} minHeight={90}>
        {noUsage ? (
          <EmptyState
            message="No usage found"
            description="This client has no activity in the selected workspace and period."
          />
        ) : (
          kpis.data && (
            <>
              <KpiStrip items={toKpiItems(kpis.data, totalTokens, range)} />

              <div className={styles.panels}>
                <PanelFrame
                  left={
                    <Panel eyebrow="Spend" title={range === '24h' ? 'Hourly spend' : 'Daily spend'} right={<DailyChartLegend />}>
                      <Section
                        isLoading={cost.isLoading || errors.isLoading}
                        isError={cost.isError || errors.isError}
                        minHeight={200}
                      >
                        <DailyChart series={dailySeries} />
                      </Section>
                    </Panel>
                  }
                  right={
                    <Panel eyebrow="Models" title="Where it goes" scroll={!stacked}>
                      <Section isLoading={models.isLoading} isError={models.isError} minHeight={200}>
                        <ModelsList models={modelRows} />
                      </Section>
                    </Panel>
                  }
                />
              </div>

              <SectionHead
                title="Workflows"
                hint="What this client runs, and what each workflow costs. Select one to open it."
              />
              <Section isLoading={workflows.isLoading} isError={workflows.isError}>
                <Breakdown
                  rows={workflowRows}
                  totalCost={sumCost(workflowRows)}
                  kind="workflow"
                  sortBy={sortBy}
                  setSortBy={setSortBy}
                  onRowClick={(row) => openWorkflow(row.name)}
                />
              </Section>
            </>
          )
        )}
      </Section>

      <SectionHead title="Recent traces" hint="The client's latest runs, newest first." />
      <Section isLoading={traces.isLoading} isError={traces.isError}>
        <RunsTable
          runs={(traces.data?.items ?? []).map((t) => ({ ...toRunRow(t), name: t.name || undefined }))}
          onOpen={openTrace}
          emptyText="No traces in this period."
        />
      </Section>
    </div>
  );
}
