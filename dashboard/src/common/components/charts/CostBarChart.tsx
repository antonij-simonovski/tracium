import { useRef, useState } from 'react';
import type { CostPoint, ChartMarker } from '../../interfaces';
import { useResize } from '../../hooks/useResize';

export interface CostBarChartProps {
  series: CostPoint[];
  height?: number;
  // Optional in-place anomaly flags: each tints its bucket's bar and draws a
  // dashed rule; a labelled marker also renders a clickable flag near the top.
  markers?: ChartMarker[];
}

export function CostBarChart({ series, height = 220, markers = [] }: CostBarChartProps) {
  const [hover, setHover] = useState<number | null>(null);
  const ref = useRef<HTMLDivElement>(null);
  const width = useResize(ref);
  const markerByIndex = new Map(markers.map((m) => [m.index, m]));

  const max = (series.length ? Math.max(...series.map((d) => d.value)) : 0) * 1.1 || 1;
  const pad = { top: 20, right: 8, bottom: 32, left: 56 };
  const innerW = Math.max(1, width - pad.left - pad.right);
  const innerH = height - pad.top - pad.bottom;
  const gap = innerW / series.length;
  const barW = gap * 0.55;
  const ticks = 4;
  // Thin x-axis labels so they never overlap: keep every Nth label, where N is
  // chosen from how many fit at ~34px each (e.g. 30 daily buckets → every 3rd).
  const labelStep = Math.max(1, Math.ceil(34 / gap));

  return (
    <div ref={ref} style={{ position: 'relative', width: '100%' }}>
      <svg width={width} height={height} style={{ display: 'block' }}>
        {Array.from({ length: ticks + 1 }, (_, i) => {
          const v = (max / ticks) * i;
          const y = pad.top + innerH - (v / max) * innerH;
          return (
            <g key={i}>
              <line
                x1={pad.left}
                x2={width - pad.right}
                y1={y}
                y2={y}
                stroke="var(--chart-grid)"
              />
              <text
                x={pad.left - 10}
                y={y + 4}
                fill="var(--muted)"
                fontSize="12"
                textAnchor="end"
              >
                ${v.toFixed(2)}
              </text>
            </g>
          );
        })}

        {series.map((d, i) => {
          const x = pad.left + i * gap + (gap - barW) / 2;
          const h = (d.value / max) * innerH;
          const y = pad.top + innerH - h;
          const isHover = hover === i;
          const marker = markerByIndex.get(i);
          return (
            <g
              key={i}
              onMouseEnter={() => setHover(i)}
              onMouseLeave={() => setHover(null)}
              style={{ cursor: 'pointer' }}
            >
              <rect
                x={pad.left + i * gap}
                y={pad.top}
                width={gap}
                height={innerH}
                fill="transparent"
              />
              <rect
                x={x}
                y={y}
                width={barW}
                height={Math.max(2, h)}
                rx={3}
                fill={marker ? marker.color : 'var(--accent)'}
                opacity={isHover ? 1 : marker ? 0.95 : 0.85}
              />
              {i % labelStep === 0 && (
                <text
                  x={x + barW / 2}
                  y={height - 10}
                  textAnchor="middle"
                  fill="var(--muted)"
                  fontSize="12"
                >
                  {d.label}
                </text>
              )}
            </g>
          );
        })}

        {width > 0 &&
          markers.map((m) => {
            if (m.index < 0 || m.index >= series.length) return null;
            const x = pad.left + m.index * gap + gap / 2;
            return (
              <g key={`mk-${m.index}-${m.label ?? ''}`} style={{ pointerEvents: 'none' }}>
                <line
                  x1={x}
                  x2={x}
                  y1={pad.top}
                  y2={pad.top + innerH}
                  stroke={m.color}
                  strokeOpacity="0.6"
                  strokeDasharray="4 3"
                />
                <circle cx={x} cy={pad.top} r={3.5} fill={m.color} />
              </g>
            );
          })}

        {hover !== null && (() => {
          const d = series[hover];
          const x = pad.left + hover * gap + gap / 2;
          const h = (d.value / max) * innerH;
          const y = pad.top + innerH - h;
          const tx = Math.min(Math.max(x, pad.left + 70), width - pad.right - 70);
          return (
            <g>
              <line
                x1={x}
                x2={x}
                y1={pad.top}
                y2={pad.top + innerH}
                stroke="var(--foreground)"
                strokeOpacity="0.12"
                strokeDasharray="3 3"
              />
              <g transform={`translate(${tx}, ${Math.max(y - 54, pad.top)})`}>
                <rect
                  x="-70"
                  y="0"
                  width="140"
                  height="44"
                  rx="8"
                  fill="var(--chart-tooltip-bg)"
                  stroke="var(--border)"
                />
                <text x="-58" y="18" fill="var(--muted)" fontSize="12">
                  {d.label}
                </text>
                <text
                  x="-58"
                  y="35"
                  fill="var(--foreground)"
                  fontSize="15"
                  fontWeight="600"
                >
                  ${d.value.toFixed(4)}
                </text>
              </g>
            </g>
          );
        })()}
      </svg>

      {width > 0 &&
        withoutOverlap(markers.filter((m) => m.label && m.index >= 0 && m.index < series.length), (m) => pad.left + m.index * gap + gap / 2)
          .map((m) => {
            const x = pad.left + m.index * gap + gap / 2;
            // Keep the flag inside the plot; anchor point is its center.
            const left = Math.min(Math.max(x, pad.left + 4), width - 4);
            return (
              <button
                key={`fl-${m.index}-${m.label}`}
                onClick={m.onClick}
                title={m.label}
                style={{
                  position: 'absolute',
                  left,
                  top: 2,
                  transform: 'translateX(-50%)',
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: 6,
                  maxWidth: '90%',
                  padding: '4px 9px',
                  borderRadius: 8,
                  fontSize: 12,
                  lineHeight: 1.2,
                  whiteSpace: 'nowrap',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  color: m.color,
                  background: `color-mix(in srgb, ${m.color} 14%, transparent)`,
                  border: `1px solid color-mix(in srgb, ${m.color} 45%, transparent)`,
                  cursor: m.onClick ? 'pointer' : 'default',
                }}
              >
                <span style={{ width: 6, height: 6, borderRadius: '50%', background: m.color, flex: 'none' }} />
                {m.label}
              </button>
            );
          })}
    </div>
  );
}

// Keeps the most recent flags and drops any that would sit on top of one already
// placed; their dashed rules still draw.
function withoutOverlap(markers: ChartMarker[], xOf: (m: ChartMarker) => number): ChartMarker[] {
  const kept: { x: number; half: number }[] = [];
  return [...markers]
    .sort((a, b) => b.index - a.index)
    .filter((m) => {
      const x = xOf(m);
      const half = 18 + (m.label?.length ?? 0) * 3.6;
      if (kept.some((k) => Math.abs(k.x - x) < k.half + half + 6)) return false;
      kept.push({ x, half });
      return true;
    });
}
