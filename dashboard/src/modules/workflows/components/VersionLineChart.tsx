import { useRef, useState } from 'react';
import { useResize } from '../../../common';
import type { ChartMarker } from '../../../common/interfaces';

type Series = (number | null)[];

export interface ChartMetric {
  title: string;
  base: Series;
  candidate: Series;
  fmt: (v: number) => string;
}

interface VersionLineChartProps {
  labels: string[];
  // One or two metrics; the second gets the right axis and dashed lines.
  metrics: ChartMetric[];
  baseLabel: string;
  candidateLabel: string;
  // Bucket range both versions ran in, shaded behind the lines.
  band?: [number, number] | null;
  markers?: ChartMarker[];
  height?: number;
}

const BASE_COLOR = 'var(--foreground)';
const CANDIDATE_COLOR = 'var(--accent)';
export const SECOND_METRIC_COLOR = 'var(--info)';

function scale(m: ChartMetric) {
  const all = [...m.base, ...m.candidate].filter((v): v is number => v != null);
  const hi = all.length ? Math.max(...all) : 1;
  const lo = all.length ? Math.min(...all) : 0;
  const spread = hi - lo || hi || 1;
  return { max: hi + spread * 0.2, min: Math.max(0, lo - spread * 0.5) };
}

export function VersionLineChart({ labels, metrics, baseLabel, candidateLabel, band, markers = [], height = 240 }: VersionLineChartProps) {
  const [hover, setHover] = useState<number | null>(null);
  const ref = useRef<HTMLDivElement>(null);
  const width = useResize(ref);

  const pad = { top: 20, right: 56, bottom: 32, left: 56 };
  const n = labels.length;
  const innerW = Math.max(1, width - pad.left - pad.right);
  const innerH = height - pad.top - pad.bottom;
  const step = innerW / Math.max(1, n);
  const dual = metrics.length > 1;
  const scales = metrics.map((m) => {
    const { min, max } = scale(m);
    return { min, max, y: (v: number) => pad.top + innerH - ((v - min) / (max - min || 1)) * innerH };
  });
  const x = (i: number) => pad.left + (i + 0.5) * step;
  const ticks = 3;
  const labelStep = Math.max(1, Math.ceil(58 / step));

  const path = (s: Series, y: (v: number) => number) => {
    let d = '';
    let pen = false;
    s.forEach((v, i) => {
      if (v == null) {
        pen = false;
        return;
      }
      d += `${pen ? 'L' : 'M'}${x(i).toFixed(1)},${y(v).toFixed(1)} `;
      pen = true;
    });
    return d;
  };
  const last = (s: Series) => {
    for (let i = s.length - 1; i >= 0; i--) if (s[i] != null) return i;
    return -1;
  };

  const lines = metrics.flatMap((m, mi) => [
    { s: m.base, color: mi ? SECOND_METRIC_COLOR : BASE_COLOR, opacity: mi ? 0.45 : 0.55, width: 2, label: baseLabel, key: `${mi}-base`, mi, fmt: m.fmt, y: scales[mi].y },
    { s: m.candidate, color: mi ? SECOND_METRIC_COLOR : CANDIDATE_COLOR, opacity: 1, width: 2.25, label: candidateLabel, key: `${mi}-candidate`, mi, fmt: m.fmt, y: scales[mi].y },
  ]);
  const tipW = dual ? 190 : 160;
  const tipH = 28 + lines.length * 16 + (dual ? 2 * 16 : 0);

  return (
    <div ref={ref} style={{ position: 'relative', width: '100%' }} onMouseLeave={() => setHover(null)}>
      <svg width={width} height={height} style={{ display: 'block', overflow: 'visible' }}>
        {band && (
          <rect
            x={pad.left + band[0] * step}
            y={pad.top}
            width={(band[1] - band[0] + 1) * step}
            height={innerH}
            fill="color-mix(in srgb, var(--info) 9%, transparent)"
            stroke="color-mix(in srgb, var(--info) 45%, transparent)"
            strokeDasharray="4 3"
          />
        )}
        {Array.from({ length: ticks + 1 }, (_, i) => {
          const [left, right] = scales;
          const v = left.min + ((left.max - left.min) * i) / ticks;
          const yy = left.y(v);
          return (
            <g key={i}>
              <line x1={pad.left} x2={width - pad.right} y1={yy} y2={yy} stroke="var(--chart-grid)" />
              <text x={pad.left - 10} y={yy + 4} fill="var(--muted)" fontSize="12" textAnchor="end">
                {metrics[0].fmt(v)}
              </text>
              {right && (
                <text x={width - pad.right + 10} y={yy + 4} fill={SECOND_METRIC_COLOR} fontSize="12">
                  {metrics[1].fmt(right.min + ((right.max - right.min) * i) / ticks)}
                </text>
              )}
            </g>
          );
        })}

        {markers.map((m) => (
          <g key={`mk-${m.index}-${m.label}`}>
            <line x1={x(m.index)} x2={x(m.index)} y1={pad.top} y2={pad.top + innerH} stroke={m.color} strokeOpacity="0.6" strokeDasharray="4 3" />
            {m.label && (
              <text x={x(m.index)} y={pad.top - 6} fill={m.color} fontSize="12" textAnchor="middle" fontFamily="var(--font-mono)">
                {m.label}
              </text>
            )}
          </g>
        ))}

        {lines.map((l) => (
          <g key={l.key} opacity={l.opacity}>
            <path d={path(l.s, l.y)} stroke={l.color} strokeWidth={l.width} strokeDasharray={l.mi ? '6 4' : undefined} fill="none" strokeLinejoin="round" strokeLinecap="round" />
            {step >= 10 &&
              l.s.map((v, i) =>
                v == null ? null : <circle key={i} cx={x(i)} cy={l.y(v)} r={3.5} fill="var(--background)" stroke={l.color} strokeWidth={1.5} />,
              )}
          </g>
        ))}

        {/* The right axis takes the end-label space when two metrics share the chart. */}
        {!dual && lines.map((l) => {
          const j = last(l.s);
          // The label would sit on the other line where it carries on right after.
          const crowded = lines.some((o) => o !== l && o.s.slice(j + 1, j + 4).some((v) => v != null));
          if (j < 0 || crowded) return null;
          return (
            <text
              key={`end-${l.key}`}
              x={x(j) + 8}
              y={l.y(l.s[j]!) + 4}
              fill={l.color === BASE_COLOR ? 'var(--muted)' : l.color}
              fontSize="12"
              fontWeight="600"
            >
              {l.fmt(l.s[j]!)}
            </text>
          );
        })}

        {labels.map((label, i) => (
          <g key={i}>
            <rect x={pad.left + i * step} y={pad.top} width={step} height={innerH} fill="transparent" onMouseEnter={() => setHover(i)} />
            {i % labelStep === 0 && (
              <text x={x(i)} y={height - 10} textAnchor="middle" fill="var(--muted)" fontSize="12">
                {label}
              </text>
            )}
          </g>
        ))}

        {hover !== null && (() => {
          const half = tipW / 2;
          const tx = Math.min(Math.max(x(hover), pad.left + half), width - pad.right - half);
          let rowY = 20;
          return (
            <g style={{ pointerEvents: 'none' }}>
              <line x1={x(hover)} x2={x(hover)} y1={pad.top} y2={pad.top + innerH} stroke="var(--foreground)" strokeOpacity="0.12" strokeDasharray="3 3" />
              <g transform={`translate(${tx}, ${pad.top})`}>
                <rect x={-half} y="0" width={tipW} height={tipH} rx="8" fill="var(--chart-tooltip-bg)" stroke="var(--border)" />
                <text x={12 - half} y="18" fill="var(--muted)" fontSize="12">{labels[hover]}</text>
                {lines.map((l) => {
                  const title = dual && l.key.endsWith('-base') ? (rowY += 16) : null;
                  const ry = (rowY += 16);
                  const v = l.s[hover];
                  return (
                    <g key={l.key} fontSize="13">
                      {title != null && (
                        <text x={12 - half} y={title} fill={l.mi ? SECOND_METRIC_COLOR : 'var(--muted)'} fontSize="12">
                          {metrics[l.mi].title}{l.mi ? ' (right)' : ''}
                        </text>
                      )}
                      <text x={12 - half} y={ry} fill={l.color === BASE_COLOR ? 'var(--muted)' : l.color} opacity={l.mi && l.key.endsWith('-base') ? 0.7 : 1} fontFamily="var(--font-mono)">
                        {l.label.length > 12 ? `${l.label.slice(0, 11)}…` : l.label}
                      </text>
                      <text x={half - 12} y={ry} fill="var(--foreground)" textAnchor="end" fontWeight="500">
                        {v == null ? '—' : l.fmt(v)}
                      </text>
                    </g>
                  );
                })}
              </g>
            </g>
          );
        })()}
      </svg>
    </div>
  );
}
