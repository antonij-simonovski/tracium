import { useMemo, useState } from 'react';
import {
  IconArrowDown,
  IconArrowUp,
  IconSearch,
  LastUpdated,
  Sparkline,
  StatTile,
  fmtCost,
  fmtNum,
  fmtMs,
  plural,
  rangeLabel,
} from '../../../common';
import hover from '../../../common/styles/hover.module.css';
import type { Workflow } from '../interfaces';
import { ERR_BAD, ERR_WARN } from '../utils';

export interface WorkflowsPageProps {
  workflows: Workflow[];
  range?: string;
  setView: (v: string) => void;
  setSelected: (updater: (prev: Record<string, string>) => Record<string, string>) => void;
  workspaceName?: string;
  updatedAt?: number;
}

type SortKey = keyof Pick<Workflow, 'name' | 'calls' | 'cost' | 'avg_latency_ms' | 'error_rate'>;
type SortDir = 'asc' | 'desc';

const GRID_COLS = '2fr 1fr 100px 100px 100px 80px';

function SortHeader({
  label,
  k,
  sortKey,
  sortDir,
  onSort,
  align = 'left',
}: {
  label: string;
  k: SortKey;
  sortKey: SortKey;
  sortDir: SortDir;
  onSort: (k: SortKey) => void;
  align?: 'left' | 'right';
}) {
  const active = sortKey === k;
  return (
    <button
      onClick={() => onSort(k)}
      aria-label={active ? `${label}, sorted ${sortDir === 'asc' ? 'ascending' : 'descending'}` : `Sort by ${label}`}
      style={{
        background: 'transparent',
        border: 'none',
        padding: 0,
        fontSize: 13,
        color: active ? 'var(--foreground)' : 'var(--muted)',
        fontWeight: 500,
        display: 'flex',
        alignItems: 'center',
        gap: 4,
        justifyContent: align === 'right' ? 'flex-end' : 'flex-start',
        width: '100%',
      }}
    >
      {label} {active && (sortDir === 'asc' ? <IconArrowUp size={11} /> : <IconArrowDown size={11} />)}
    </button>
  );
}

export function WorkflowsPage({ workflows, range = '7d', setView, setSelected, workspaceName, updatedAt }: WorkflowsPageProps) {
  const [sortKey, setSortKey] = useState<SortKey>('calls');
  const [sortDir, setSortDir] = useState<SortDir>('desc');
  const [q, setQ] = useState('');

  const filtered = useMemo(() => {
    let data = workflows;
    if (q) data = data.filter(a => a.name.toLowerCase().includes(q.toLowerCase()));
    return [...data].sort((a, b) => {
      const av = a[sortKey];
      const bv = b[sortKey];
      if (typeof av === 'string' && typeof bv === 'string') {
        return sortDir === 'asc' ? av.localeCompare(bv) : bv.localeCompare(av);
      }
      return sortDir === 'asc' ? (av as number) - (bv as number) : (bv as number) - (av as number);
    });
  }, [workflows, sortKey, sortDir, q]);

  const toggleSort = (k: SortKey) => {
    if (sortKey === k) setSortDir(d => (d === 'asc' ? 'desc' : 'asc'));
    else {
      setSortKey(k);
      setSortDir('desc');
    }
  };

  const totalCalls = workflows.reduce((s, a) => s + a.calls, 0);
  const totalCost = workflows.reduce((s, a) => s + a.cost, 0);
  const topVolume = workflows.reduce<Workflow | null>((m, a) => (!m || a.calls > m.calls ? a : m), null);
  const topCost = workflows.reduce<Workflow | null>((m, a) => (!m || a.cost > m.cost ? a : m), null);

  const openWorkflow = (a: Workflow) => {
    setSelected(prev => ({ ...prev, workflow: a.name }));
    setView('workflows');
  };

  return (
    <div style={{ padding: '32px 40px 64px', maxWidth: 1480, margin: '0 auto' }}>
      <div
        style={{
          display: 'flex',
          alignItems: 'flex-end',
          justifyContent: 'space-between',
          marginBottom: 24,
        }}
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
          <span
            style={{
              fontSize: 12,
              fontWeight: 500,
              textTransform: 'uppercase',
              letterSpacing: '0.1em',
              color: 'var(--muted)',
            }}
          >
            {workspaceName ? `${workspaceName} · Workspace` : 'Workspace'}
          </span>
          <h1 style={{ fontSize: 26, fontWeight: 600, letterSpacing: '-0.02em', margin: 0 }}>Workflows</h1>
          <p style={{ fontSize: 14, color: 'var(--muted)', margin: 0 }}>
            {plural(workflows.length, 'active workflow')} · {fmtNum(totalCalls)} runs · {fmtCost(totalCost)} spend
          </p>
        </div>
        {updatedAt != null && <LastUpdated at={updatedAt} />}
      </div>

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(3, 1fr)',
          borderTop: '1px solid var(--border)',
          borderBottom: '1px solid var(--border)',
          margin: '4px 0 28px',
        }}
      >
        <StatTile isFirst label="Active workflows" value={workflows.length} />
        <StatTile label="Highest volume" value={topVolume?.name ?? '—'} sub={topVolume ? `${fmtNum(topVolume.calls)} runs` : undefined} />
        <StatTile label="Most expensive" value={topCost?.name ?? '—'} sub={topCost ? fmtCost(topCost.cost) : undefined} />
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 14 }}>
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 8,
            padding: '7px 12px',
            background: 'var(--surface)',
            border: '1px solid var(--border)',
            borderRadius: 8,
            flex: 1,
            maxWidth: 320,
          }}
        >
          <IconSearch size={14} style={{ color: 'var(--muted)', flexShrink: 0 }} />
          <input
            value={q}
            onChange={e => setQ(e.target.value)}
            placeholder="Filter workflows..."
            aria-label="Filter workflows"
            style={{
              flex: 1,
              background: 'transparent',
              border: 'none',
              outline: 'none',
              color: 'var(--foreground)',
              fontSize: 14,
            }}
          />
        </div>
      </div>

      <div>
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: GRID_COLS,
            gap: 14,
            padding: '10px 4px',
            borderBottom: '1px solid var(--border)',
          }}
        >
          <SortHeader label="Workflow" k="name" sortKey={sortKey} sortDir={sortDir} onSort={toggleSort} />
          <span style={{ fontSize: 13, color: 'var(--muted)' }}>Trend · {rangeLabel(range)}</span>
          <SortHeader label="Calls" k="calls" sortKey={sortKey} sortDir={sortDir} onSort={toggleSort} align="right" />
          <SortHeader label="Cost" k="cost" sortKey={sortKey} sortDir={sortDir} onSort={toggleSort} align="right" />
          <SortHeader label="Avg latency" k="avg_latency_ms" sortKey={sortKey} sortDir={sortDir} onSort={toggleSort} align="right" />
          <SortHeader label="Error" k="error_rate" sortKey={sortKey} sortDir={sortDir} onSort={toggleSort} align="right" />
        </div>

        {filtered.length === 0 ? (
          <div style={{ padding: '60px 4px', textAlign: 'center', color: 'var(--muted)', fontSize: 14 }}>
            No workflows match.
          </div>
        ) : (
          filtered.map((a, i) => (
            <WorkflowRow
              key={a.name}
              workflow={a}
              isLast={i === filtered.length - 1}
              onOpen={() => openWorkflow(a)}
            />
          ))
        )}
      </div>
    </div>
  );
}

function WorkflowRow({ workflow: a, isLast, onOpen }: { workflow: Workflow; isLast: boolean; onOpen: () => void }) {
  const errColor =
    a.error_rate > ERR_BAD ? 'var(--error)' : a.error_rate > ERR_WARN ? 'var(--warning)' : 'var(--muted)';

  return (
    <button
      onClick={onOpen}
      className={hover.row}
      style={{
        display: 'grid',
        gridTemplateColumns: GRID_COLS,
        gap: 14,
        alignItems: 'center',
        width: '100%',
        padding: '14px 4px',
        border: 'none',
        textAlign: 'left',
        cursor: 'pointer',
        borderBottom: isLast
          ? 'none'
          : '1px solid color-mix(in srgb, var(--border) 55%, transparent)',
      }}
    >
      <span style={{ fontSize: 14.5, fontWeight: 500, color: 'var(--foreground)' }}>{a.name}</span>
      <Sparkline data={a.trend} width={140} height={26} color="var(--accent)" fillOpacity={0.1} />
      <span style={{ fontSize: 14, fontVariantNumeric: 'tabular-nums', textAlign: 'right', color: 'var(--foreground)' }}>
        {fmtNum(a.calls)}
      </span>
      <span style={{ fontSize: 14, fontVariantNumeric: 'tabular-nums', textAlign: 'right', color: 'var(--foreground)' }}>
        {fmtCost(a.cost)}
      </span>
      <span style={{ fontSize: 14, fontVariantNumeric: 'tabular-nums', textAlign: 'right', color: 'var(--muted)' }}>
        {a.avg_latency_ms > 0 ? fmtMs(a.avg_latency_ms) : '—'}
      </span>
      <span
        style={{
          fontSize: 14,
          fontVariantNumeric: 'tabular-nums',
          textAlign: 'right',
          color: errColor,
          fontWeight: a.error_rate > ERR_WARN ? 500 : 400,
        }}
      >
        {(a.error_rate * 100).toFixed(1)}%
      </span>
    </button>
  );
}
