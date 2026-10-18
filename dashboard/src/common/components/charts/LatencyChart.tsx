import { useRef, useState } from 'react';
import type { LatencyPoint, ChartMarker } from '../../interfaces';
import { useResize } from '../../hooks/useResize';

export interface LatencyChartProps {
  series: LatencyPoint[];
  height?: number;
  // Dashed rules at the given buckets, e.g. version releases.
  markers?: ChartMarker[];
}

export function LatencyChart({ series, height = 220, markers = [] }: LatencyChartProps) {
  const [hover, setHover] = useState<number | null>(null);
  const ref = useRef<HTMLDivElement>(null);
  const width = useResize(ref);

  const pad = { top: 20, right: 16, bottom: 32, left: 56 };
  const innerW = Math.max(1, width - pad.left - pad.right);
  const innerH = height - pad.top - pad.bottom;
  const p99Values = series.map((d) => d.p99).filter((v): v is number => v != null);
  const max = (p99Values.length ? Math.max(...p99Values) : 1) * 1.05;
  const step = innerW / (series.length - 1 || 1);
  const ticks = 4;
  // Thin x-axis labels so they never overlap: keep every Nth label, where N is
  // chosen from how many fit at ~34px each (e.g. 30 daily buckets → every 3rd).
  const labelStep = Math.max(1, Math.ceil(34 / step));

  type Pt = { x: number; y: number };
  // Split a percentile into the contiguous runs of buckets that actually have
  // data; null buckets (no runs) break the line so the chart shows a real gap
  // instead of a dip to the floor.
  function segmentsOf(key: keyof Pick<LatencyPoint, 'p50' | 'p95' | 'p99'>): Pt[][] {
    const segs: Pt[][] = [];
    let seg: Pt[] = [];
    series.forEach((d, i) => {
      const v = d[key];
      if (v == null) {
        if (seg.length) segs.push(seg);
        seg = [];
        return;
      }
      seg.push({ x: pad.left + i * step, y: pad.top + innerH - (v / max) * innerH });
    });
    if (seg.length) segs.push(seg);
    return segs;
  }

  const linePath = (segs: Pt[][]): string =>
    segs
      .map((seg) => seg.map((p, i) => (i === 0 ? 'M' : 'L') + p.x.toFixed(1) + ',' + p.y.toFixed(1)).join(' '))
      .join(' ');

  const p50Segs = segmentsOf('p50');
  const p95Segs = segmentsOf('p95');
  const p99Segs = segmentsOf('p99');
  const baseY = (pad.top + innerH).toFixed(1);
  const areaPath = p99Segs
    .map((seg) => linePath([seg]) + ` L${seg[seg.length - 1].x.toFixed(1)},${baseY} L${seg[0].x.toFixed(1)},${baseY} Z`)
    .join(' ');

  // An isolated bucket — one non-null point with null neighbours on both sides —
  // becomes a length-1 segment, which draws no line (a lone `M` has nothing to
  // connect to). A single-trace workflow hits this and the whole series vanishes, so
  // render a dot for every singleton segment to keep the point visible.
  const isolatedPoints = (segs: Pt[][]): Pt[] => segs.filter((s) => s.length === 1).map((s) => s[0]);

  return (
    <div
      ref={ref}
      style={{ position: 'relative', width: '100%' }}
      onMouseLeave={() => setHover(null)}
    >
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
                {v.toFixed(1)}s
              </text>
            </g>
          );
        })}

        <path d={areaPath} fill="var(--accent)" opacity="0.06" />

        {markers
          .filter((m) => m.index >= 0 && m.index < series.length)
          .map((m) => (
            <line
              key={`mk-${m.index}-${m.label ?? ''}`}
              x1={pad.left + m.index * step}
              x2={pad.left + m.index * step}
              y1={pad.top}
              y2={pad.top + innerH}
              stroke={m.color}
              strokeOpacity="0.6"
              strokeDasharray="4 3"
            />
          ))}

        <path
          d={linePath(p99Segs)}
          stroke="var(--warning)"
          strokeWidth="1.5"
          fill="none"
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeDasharray="4 4"
        />
        <path
          d={linePath(p95Segs)}
          stroke="var(--accent)"
          strokeWidth="2"
          fill="none"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <path
          d={linePath(p50Segs)}
          stroke="var(--foreground)"
          strokeOpacity="0.4"
          strokeWidth="1.5"
          fill="none"
          strokeLinecap="round"
          strokeLinejoin="round"
        />

        {/* Dots for isolated points, which the line paths above can't draw */}
        {isolatedPoints(p99Segs).map((p, i) => (
          <circle key={`i99-${i}`} cx={p.x} cy={p.y} r={2.5} fill="var(--warning)" />
        ))}
        {isolatedPoints(p50Segs).map((p, i) => (
          <circle key={`i50-${i}`} cx={p.x} cy={p.y} r={2.5} fill="var(--foreground)" opacity="0.4" />
        ))}
        {isolatedPoints(p95Segs).map((p, i) => (
          <circle key={`i95-${i}`} cx={p.x} cy={p.y} r={3} fill="var(--accent)" />
        ))}

        {series.map((d, i) => {
          const x = pad.left + i * step;
          return (
            <g key={i}>
              <rect
                x={x - step / 2}
                y={pad.top}
                width={step}
                height={innerH}
                fill="transparent"
                onMouseEnter={() => setHover(i)}
              />
              {hover === i && d.p95 != null && (
                <circle
                  cx={x}
                  cy={pad.top + innerH - (d.p95 / max) * innerH}
                  r={4}
                  fill="var(--accent)"
                />
              )}
              {hover === i && d.p50 != null && (
                <circle
                  cx={x}
                  cy={pad.top + innerH - (d.p50 / max) * innerH}
                  r={3.5}
                  fill="var(--foreground)"
                  opacity="0.7"
                />
              )}
              {hover === i && d.p99 != null && (
                <circle
                  cx={x}
                  cy={pad.top + innerH - (d.p99 / max) * innerH}
                  r={3.5}
                  fill="var(--warning)"
                />
              )}
              {i % labelStep === 0 && (
                <text
                  x={x}
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

        {hover !== null && (() => {
          const d = series[hover];
          const x = pad.left + hover * step;
          const tx = Math.min(Math.max(x, pad.left + 80), width - pad.right - 80);
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
              <g transform={`translate(${tx}, ${pad.top})`}>
                <rect
                  x="-80"
                  y="0"
                  width="160"
                  height="82"
                  rx="8"
                  fill="var(--chart-tooltip-bg)"
                  stroke="var(--border)"
                />
                <text x="-68" y="18" fill="var(--muted)" fontSize="12">
                  {d.label}
                </text>
                <g fontSize="13">
                  <text x="-68" y="36" fill="var(--muted)">
                    p50
                  </text>
                  <text x="68" y="36" fill="var(--foreground)" textAnchor="end" fontWeight="500">
                    {d.p50 == null ? '—' : d.p50.toFixed(2) + 's'}
                  </text>
                  <text x="-68" y="52" fill="var(--accent)">
                    p95
                  </text>
                  <text x="68" y="52" fill="var(--accent)" textAnchor="end" fontWeight="500">
                    {d.p95 == null ? '—' : d.p95.toFixed(2) + 's'}
                  </text>
                  <text x="-68" y="68" fill="var(--warning)">
                    p99
                  </text>
                  <text x="68" y="68" fill="var(--warning)" textAnchor="end" fontWeight="500">
                    {d.p99 == null ? '—' : d.p99.toFixed(2) + 's'}
                  </text>
                </g>
              </g>
            </g>
          );
        })()}
      </svg>
    </div>
  );
}
