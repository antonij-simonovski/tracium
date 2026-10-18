import { Sparkline, fmtNum, fmtCost, rangeLabel } from '../../../common';
import hover from '../../../common/styles/hover.module.css';
import { SectionRule } from './SectionRule';

export interface TopWorkflowRow {
  name: string;
  calls: number;
  cost: number;
  trend?: number[];
}

export function TopWorkflows({
  workflows,
  onSelectWorkflow,
  range = '7d',
}: {
  workflows: TopWorkflowRow[];
  onSelectWorkflow: (name: string) => void;
  range?: string;
}) {
  const maxCalls = Math.max(...workflows.map((a) => a.calls), 1);

  return (
    <div>
      <SectionRule eyebrow="Volume" title="Most used workflows" subtitle={rangeLabel(range)} />
      <div style={{ margin: '-4px 0' }}>
        {workflows.map((a, i) => {
          const pct = (a.calls / maxCalls) * 100;
          return (
            <button
              key={a.name}
              onClick={() => onSelectWorkflow(a.name)}
              className={hover.row}
              style={{
                display: 'grid',
                gridTemplateColumns: '1fr auto auto',
                alignItems: 'center',
                gap: 18,
                width: '100%',
                padding: '14px 4px',
                border: 'none',
                textAlign: 'left',
                borderBottom:
                  i < workflows.length - 1 ? '1px solid color-mix(in srgb, var(--border) 55%, transparent)' : 'none',
              }}
            >
              <div style={{ display: 'flex', flexDirection: 'column', gap: 7, minWidth: 0 }}>
                <span style={{ fontSize: 14.5, fontWeight: 500, color: 'var(--foreground)' }}>{a.name}</span>
                <div
                  style={{
                    position: 'relative',
                    height: 2,
                    background: 'color-mix(in srgb, var(--border) 80%, transparent)',
                    borderRadius: 2,
                    overflow: 'hidden',
                  }}
                >
                  <div
                    style={{ position: 'absolute', inset: 0, width: pct + '%', background: 'var(--accent)', opacity: 0.7, borderRadius: 2 }}
                  />
                </div>
              </div>
              {a.trend ? <Sparkline data={a.trend} width={60} height={24} color="var(--accent)" fillOpacity={0.06} /> : <span />}
              <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: 2, minWidth: 84 }}>
                <span style={{ fontSize: 14.5, fontWeight: 500, color: 'var(--foreground)', fontVariantNumeric: 'tabular-nums' }}>
                  {fmtNum(a.calls)}
                </span>
                <span style={{ fontSize: 13, color: 'var(--muted)', fontVariantNumeric: 'tabular-nums' }}>{fmtCost(a.cost)}</span>
              </div>
            </button>
          );
        })}
      </div>
    </div>
  );
}
