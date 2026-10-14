import type { VersionSource } from '../interfaces';
import { SOURCE_COLOR } from '../utils';

export function VersionTag({ source }: { source: VersionSource }) {
  if (!source) return null;
  const color = SOURCE_COLOR[source];
  return (
    <span
      style={{
        padding: '1px 5px',
        borderRadius: 3,
        fontFamily: 'inherit',
        fontSize: 10.5,
        fontWeight: 600,
        letterSpacing: '0.08em',
        flexShrink: 0,
        color,
        background: `color-mix(in srgb, ${color} 14%, transparent)`,
        border: `1px solid color-mix(in srgb, ${color} 25%, transparent)`,
      }}
    >
      {source}
    </span>
  );
}
