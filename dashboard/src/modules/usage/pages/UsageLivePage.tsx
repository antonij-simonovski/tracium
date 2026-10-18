import { useState } from 'react';
import {
  EmptyState,
  KpiStrip,
  Panel,
  PanelFrame,
  Section,
  SectionHead,
  isLongRange,
  periodLabel,
  useMaxWidth,
  BREAKPOINTS,
} from '../../../common';
import type { UserId } from '../../../common/ids';
import { useKpis, useCostSeries, useErrorSeries } from '../../overview/hooks/useMetrics';
import {
  DailyChart,
  DailyChartLegend,
  TokenChart,
  TokenChartLegend,
  TokenMix,
  ModelsList,
  UsageMasthead,
  Breakdown,
  TabPill,
} from '../components';
import type { BreakdownTab, SortKey } from '../components';
import {
  useModelCosts,
  useTokenSeries,
  useUserUsage,
  useWorkflowUsage,
  useAttributeKeys,
  useAttributeUsage,
} from '../hooks/useUsage';
import type { UserSummary, AttributeSummary, UserUsage, AttributeUsage } from '../interfaces';
import {
  UNATTRIBUTED,
  toDailySeries,
  toTokenSeries,
  toModelSummaries,
  toWorkflowSummaries,
  sumCost,
  spendTile,
  runsTile,
  tokensTile,
} from '../utils';

interface UsageLivePageProps {
  range: string;
  setView: (v: string) => void;
  setSelected: (updater: (prev: Record<string, string>) => Record<string, string>) => void;
}

const toUserSummaries = (users: UserUsage[]): UserSummary[] =>
  users.map((t) => ({
    id: (t.user_id || UNATTRIBUTED) as UserId,
    name: t.user_id || 'default',
    cost: t.cost,
    costPrev: t.cost_prev,
    runs: t.runs,
    runsPrev: t.runs_prev,
    avg: t.runs > 0 ? t.cost / t.runs : 0,
    trend: t.trend ?? [],
  }));

const toAttributeSummaries = (rows: AttributeUsage[]): AttributeSummary[] =>
  rows.map((r) => ({
    name: r.value || UNATTRIBUTED,
    cost: r.cost,
    // The attribute endpoint carries no previous-period figures.
    costPrev: 0,
    runs: r.runs,
    runsPrev: 0,
    avg: r.runs > 0 ? r.cost / r.runs : 0,
  }));

export function UsageLivePage({ range, setView, setSelected }: UsageLivePageProps) {
  const [tab, setTab] = useState<BreakdownTab>('user');
  const [sortBy, setSortBy] = useState<SortKey>('cost');

  const stacked = useMaxWidth(BREAKPOINTS.tablet);

  const [attrKey, setAttrKey] = useState('');

  const kpis = useKpis(range);
  const cost = useCostSeries(range);
  const errors = useErrorSeries(range);
  const models = useModelCosts(range);
  const tokens = useTokenSeries(range);
  const users = useUserUsage(range);
  const workflows = useWorkflowUsage(range);
  const attrEnabled = !isLongRange(range);
  const attrKeys = useAttributeKeys(range, attrEnabled);

  const attributeKeys = attrKeys.data?.items ?? [];
  const activeAttr = attributeKeys.includes(attrKey) ? attrKey : attributeKeys[0] ?? '';
  const view: BreakdownTab = tab === 'attribute' && !attrEnabled ? 'user' : tab;
  const attrUsage = useAttributeUsage(range, view === 'attribute' ? activeAttr : '');

  function openClient(id: string) {
    setSelected((prev) => ({ ...prev, user: id }));
    setView('user');
  }

  function handleTabChange(t: BreakdownTab) {
    setTab(t);
    setSortBy('cost');
  }

  function handleAttrSelect(k: string) {
    setAttrKey(k);
    setTab('attribute');
    setSortBy('cost');
  }

  if (kpis.isSuccess && kpis.data.cost.value === 0 && kpis.data.runs.value === 0) {
    return (
      <EmptyState
        message="No usage yet"
        description="Usage and cost metrics appear once data is flowing."
      />
    );
  }

  const dailySeries =
    cost.data && errors.data ? toDailySeries(cost.data.items, errors.data.items, range) : [];
  const tokenSeries =
    tokens.data && errors.data ? toTokenSeries(tokens.data.items, errors.data.items, range) : [];
  const modelRows = models.data ? toModelSummaries(models.data.items) : [];
  const totalTokens = models.data
    ? modelRows.reduce((s, m) => s + m.inputTokens + m.outputTokens, 0)
    : undefined;
  const userRows = users.data ? toUserSummaries(users.data.items) : [];
  const workflowRows = workflows.data ? toWorkflowSummaries(workflows.data.items) : [];
  const attrRows = attrUsage.data ? toAttributeSummaries(attrUsage.data.items) : [];

  const userCount = users.data?.items.length ?? 0;
  const workflowCount = workflows.data?.items.length ?? 0;
  const subtitle = `${periodLabel(range)} · ${userCount} active clients, ${workflowCount} workflows`;

  // Freshness reflects the most recent successful fetch across the page's
  // sections; 0 (nothing loaded yet) hides the badge.
  const updatedAt =
    Math.max(
      kpis.dataUpdatedAt,
      cost.dataUpdatedAt,
      errors.dataUpdatedAt,
      models.dataUpdatedAt,
      tokens.dataUpdatedAt,
      users.dataUpdatedAt,
      workflows.dataUpdatedAt,
    ) || undefined;

  return (
    <div
      style={{
        padding: 'clamp(20px, 4vw, 32px) clamp(16px, 4vw, 40px) 96px',
        maxWidth: 1480,
        margin: '0 auto',
      }}
    >
      <UsageMasthead subtitle={subtitle} updatedAt={updatedAt} />

      <Section isLoading={kpis.isLoading} isError={kpis.isError} minHeight={90}>
        {kpis.data && <KpiStrip items={[spendTile(kpis.data), runsTile(kpis.data), tokensTile(totalTokens)]} />}
      </Section>

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

      <PanelFrame
        left={
          <Panel eyebrow="Tokens" title="Tokens in vs out" right={<TokenChartLegend />}>
            <Section
              isLoading={tokens.isLoading || errors.isLoading}
              isError={tokens.isError || errors.isError}
              minHeight={200}
            >
              <TokenChart series={tokenSeries} />
            </Section>
          </Panel>
        }
        right={
          <Panel eyebrow="Mix" title="Token mix">
            <Section
              isLoading={tokens.isLoading || errors.isLoading}
              isError={tokens.isError || errors.isError}
              minHeight={200}
            >
              <TokenMix series={tokenSeries} />
            </Section>
          </Panel>
        }
      />

      <SectionHead
        title="Who's driving cost"
        hint="Dot marks the top 75% of spend: the rows worth reviewing first."
        right={
          <TabPill
            tab={view}
            setTab={handleTabChange}
            tabs={[
              { id: 'user', label: 'By client', count: userCount },
              { id: 'workflow', label: 'By workflow', count: workflowCount },
            ]}
            attribute={{
              keys: attributeKeys,
              value: activeAttr,
              onSelect: handleAttrSelect,
            }}
          />
        }
      />
      {view === 'user' && (
        <Section isLoading={users.isLoading} isError={users.isError}>
          <Breakdown
            rows={userRows}
            totalCost={sumCost(userRows)}
            kind="user"
            sortBy={sortBy}
            setSortBy={setSortBy}
            onRowClick={(row) => openClient((row as UserSummary).id)}
          />
        </Section>
      )}
      {view === 'workflow' && (
        <Section isLoading={workflows.isLoading} isError={workflows.isError}>
          <Breakdown
            rows={workflowRows}
            totalCost={sumCost(workflowRows)}
            kind="workflow"
            sortBy={sortBy}
            setSortBy={setSortBy}
          />
        </Section>
      )}
      {view === 'attribute' && (
        <Section isLoading={attrUsage.isLoading} isError={attrUsage.isError}>
          <Breakdown
            rows={attrRows}
            totalCost={sumCost(attrRows)}
            kind="attribute"
            nameLabel={activeAttr}
            sortBy={sortBy}
            setSortBy={setSortBy}
          />
        </Section>
      )}
    </div>
  );
}

