import type { CSSProperties } from 'react';

export interface MetaRowProps {
  label: string;
  value: string | number | boolean | undefined;
  mono?: boolean;
  accent?: boolean;
  onClick?: () => void;
}

export function MetaRow({ label, value, mono, accent, onClick }: MetaRowProps) {
  if (value == null || value === '') return null;
  const text = String(value);
  const valueStyle: CSSProperties = {
    fontSize: 13.5,
    color: accent || onClick ? 'var(--accent)' : 'var(--foreground)',
    fontWeight: 500,
    fontFamily: mono ? 'var(--font-mono)' : 'inherit',
    textAlign: 'right',
    whiteSpace: 'nowrap',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    maxWidth: '70%',
  };
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'baseline',
        justifyContent: 'space-between',
        gap: 12,
        padding: '8px 0',
        borderBottom: '1px solid color-mix(in srgb, var(--border) 55%, transparent)',
      }}
    >
      <span style={{ fontSize: 13, color: 'var(--muted)' }}>{label}</span>
      {onClick ? (
        <button
          onClick={onClick}
          title={text}
          style={{ ...valueStyle, padding: 0, border: 'none', background: 'none', cursor: 'pointer' }}
        >
          {text}
        </button>
      ) : (
        <span style={valueStyle} title={text}>
          {text}
        </span>
      )}
    </div>
  );
}
