export type StatTone = 'bad' | 'warn' | 'good';

const TONE_COLOR: Record<StatTone, string> = {
  bad: 'var(--error)',
  warn: 'var(--warning)',
  good: 'var(--accent)',
};

interface StatTileProps {
  label: string;
  value: string | number;
  sub?: string;
  tone?: StatTone;
  isFirst?: boolean;
}

export function StatTile({ label, value, sub, tone, isFirst }: StatTileProps) {
  return (
    <div style={{ padding: '16px 22px 18px', borderLeft: isFirst ? 'none' : '1px solid var(--border)', minWidth: 0 }}>
      <div style={{ fontSize: 13, color: 'var(--muted)', marginBottom: 8, fontWeight: 500 }}>{label}</div>
      <div
        title={String(value)}
        style={{
          fontSize: 22,
          fontWeight: 500,
          letterSpacing: '-0.02em',
          color: tone ? TONE_COLOR[tone] : 'var(--foreground)',
          fontVariantNumeric: 'tabular-nums',
          lineHeight: 1,
          whiteSpace: 'nowrap',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
        }}
      >
        {value}
      </div>
      {sub && <div style={{ fontSize: 12.5, color: 'var(--muted)', marginTop: 6 }}>{sub}</div>}
    </div>
  );
}
