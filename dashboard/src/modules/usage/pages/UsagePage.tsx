import { useState } from 'react';
import { KpiStrip, Panel, PanelFrame, SectionHead, fmtCost, fmtDelta, fmtNum, fmtTokens } from '../../../common';
import type { KpiItem } from '../../../common';
import { DailyChart, DailyChartLegend, TokenChart, TokenChartLegend, TokenMix, ModelsList, UsageMasthead, Breakdown, TabPill } from '../components';
import type { BreakdownTab, SortKey } from '../components';
import { USAGE_DATA } from '../data';

function kpiItems(data: typeof USAGE_DATA): KpiItem[] {
  const costDelta = (data.totalCost - data.totalCostPrev) / data.totalCostPrev;
  const runsDelta = (data.totalRuns - data.totalRunsPrev) / data.totalRunsPrev;
  const avgPer1k = data.totalRuns > 0 ? (data.totalCost / data.totalRuns) * 1000 : 0;
  const totalTokens = data.models.reduce((s, m) => s + m.inputTokens + m.outputTokens, 0);

  return [
    {
      label: 'Spend',
      value: fmtCost(data.totalCost),
      delta: fmtDelta(costDelta),
      deltaTone: costDelta > 0 ? 'bad' : 'good',
      hint: 'vs ' + fmtCost(data.totalCostPrev) + ' prev',
    },
    {
      label: 'Runs',
      value: fmtNum(data.totalRuns),
      delta: fmtDelta(runsDelta),
      deltaTone: 'neutral',
      hint: fmtCost(avgPer1k) + ' / 1K runs',
    },
    {
      label: 'Tokens',
      value: fmtTokens(totalTokens),
      hint: 'input + output',
    },
  ];
}

const KPI_ITEMS = kpiItems(USAGE_DATA);

export default function UsagePage() {
  const data = USAGE_DATA;
  const [tab, setTab] = useState<BreakdownTab>('user');
  const [sortBy, setSortBy] = useState<SortKey>('cost');

  function handleTabChange(t: BreakdownTab) {
    setTab(t);
    setSortBy('cost');
  }

  return (
    <div
      style={{
        padding: 'clamp(20px, 4vw, 32px) clamp(16px, 4vw, 40px) 96px',
        maxWidth: 1480,
        margin: '0 auto',
      }}
    >
      <UsageMasthead
        subtitle={`${data.range.start} – ${data.range.end} · ${data.users.length} active clients, ${data.workflows.length} workflows`}
      />

      <KpiStrip items={KPI_ITEMS} />

      <PanelFrame
        left={
          <Panel eyebrow="Spend" title="Daily spend" right={<DailyChartLegend />}>
            <DailyChart series={data.dailySeries} />
          </Panel>
        }
        right={
          <Panel eyebrow="Models" title="Where it goes">
            <ModelsList models={data.models} />
          </Panel>
        }
      />

      <PanelFrame
        left={
          <Panel eyebrow="Tokens" title="Tokens in vs out" right={<TokenChartLegend />}>
            <TokenChart series={data.tokenSeries} />
          </Panel>
        }
        right={
          <Panel eyebrow="Mix" title="Token mix">
            <TokenMix series={data.tokenSeries} />
          </Panel>
        }
      />

      <SectionHead
        title="Who's driving cost"
        hint="Dot marks the top 75% of spend: the rows worth reviewing first."
        right={
          <TabPill
            tab={tab}
            setTab={handleTabChange}
            tabs={[
              { id: 'user', label: 'By client', count: data.users.length },
              { id: 'workflow', label: 'By workflow', count: data.workflows.length },
            ]}
          />
        }
      />
      {tab === 'user' ? (
        <Breakdown
          rows={data.users}
          totalCost={data.totalCost}
          kind="user"
          sortBy={sortBy}
          setSortBy={setSortBy}
        />
      ) : (
        <Breakdown
          rows={data.workflows}
          totalCost={data.totalCost}
          kind="workflow"
          sortBy={sortBy}
          setSortBy={setSortBy}
        />
      )}
    </div>
  );
}
