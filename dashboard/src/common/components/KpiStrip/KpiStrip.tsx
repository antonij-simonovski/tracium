import { Sparkline } from '../Sparkline';
import { IconArrowUp, IconArrowDown, IconArrowRight } from '../icons';
import { useMaxWidth, BREAKPOINTS } from '../../hooks/useMediaQuery';

export type DeltaTone = 'good' | 'bad' | 'neutral';

export interface KpiItem {
  label: string;
  value: string;
  // A leading +/- picks the arrow; the sign itself isn't printed.
  delta?: string;
  deltaTone?: DeltaTone;
  sparkData?: (number | null)[];
  sparkColor?: string;
  hint?: string;
}

export const DELTA_COLOR: Record<DeltaTone, string> = {
  good: 'var(--accent)',
  bad: 'var(--error)',
  neutral: 'var(--muted)',
};

export function KpiStrip({ items }: { items: KpiItem[] }) {
  const narrow = useMaxWidth(BREAKPOINTS.mobile);
  const cols = narrow ? 2 : items.length;
  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: `repeat(${cols}, 1fr)`,
        borderTop: '1px solid var(--border)',
        borderBottom: '1px solid var(--border)',
        margin: '4px 0 32px',
      }}
    >
      {items.map((it, i) => {
        const delta = it.delta ?? '';
        return (
          <div
            key={it.label}
            style={{
              padding: '20px 24px 22px',
              borderLeft: i % cols !== 0 ? '1px solid var(--border)' : 'none',
              borderTop: i >= cols ? '1px solid var(--border)' : 'none',
              display: 'flex',
              flexDirection: 'column',
              gap: 10,
              minWidth: 0,
            }}
          >
            <span style={{ fontSize: 13, color: 'var(--muted)', fontWeight: 500 }}>{it.label}</span>
            <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 12 }}>
              <span
                style={{
                  fontSize: 30,
                  fontWeight: 500,
                  letterSpacing: '-0.025em',
                  color: 'var(--foreground)',
                  fontVariantNumeric: 'tabular-nums',
                  lineHeight: 1,
                }}
              >
                {it.value}
              </span>
              {it.sparkData && (
                <Sparkline
                  data={it.sparkData}
                  width={72}
                  height={24}
                  color={it.sparkColor ?? 'var(--accent)'}
                  fillOpacity={0.08}
                />
              )}
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13 }}>
              {(delta || it.deltaTone === 'neutral') && (
                <span
                  style={{
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: 3,
                    color: DELTA_COLOR[it.deltaTone ?? 'neutral'],
                    fontWeight: 500,
                  }}
                >
                  {delta.startsWith('+') ? (
                    <IconArrowUp size={11} />
                  ) : delta.startsWith('-') ? (
                    <IconArrowDown size={11} />
                  ) : it.deltaTone === 'neutral' ? (
                    <IconArrowRight size={11} />
                  ) : null}
                  {delta.replace(/^[+-]/, '')}
                </span>
              )}
              {it.hint && <span style={{ color: 'var(--muted)' }}>{it.hint}</span>}
            </div>
          </div>
        );
      })}
    </div>
  );
}
