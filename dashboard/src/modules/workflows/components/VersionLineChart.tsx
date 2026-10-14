import { useRef, useState } from 'react';
import { useResize } from '../../../common';
import type { ChartMarker } from '../../../common/interfaces';

type Series = (number | null)[];

interface VersionLineChartProps {
  labels: string[];
  base: Series;
  candidate: Series;
  baseLabel: string;
  candidateLabel: string;
  yFmt: (v: number) => string;
  // Bucket range both versions ran in, shaded behind the lines.
  band?: [number, number] | null;
  markers?: ChartMarker[];
  height?: number;
}

const BASE_COLOR = 'var(--foreground)';
const CANDIDATE_COLOR = 'var(--accent)';

export function VersionLineChart({ labels, base, candidate, baseLabel, candidateLabel, yFmt, band, markers = [], height = 240 }: VersionLineChartProps) {
  const [hover, setHover] = useState<number | null>(null);
  const ref = useRef<HTMLDivElement>(null);
  const width = useResize(ref);

  const pad = { top: 20, right: 56, bottom: 32, left: 56 };
  const n = labels.length;
  const innerW = Math.max(1, width - pad.left - pad.right);
  const innerH = height - pad.top - pad.bottom;
  const step = innerW / Math.max(1, n);
  const all = [...base, ...candidate].filter((v): v is number => v != null);
  const hi = all.length ? Math.max(...all) : 1;
  const lo = all.length ? Math.min(...all) : 0;
  const spread = hi - lo || hi || 1;
  const max = hi + spread * 0.2;
  const min = Math.max(0, lo - spread * 0.5);
  const x = (i: number) => pad.left + (i + 0.5) * step;
  const y = (v: number) => pad.top + innerH - ((v - min) / (max - min || 1)) * innerH;
  const ticks = 3;
  const labelStep = Math.max(1, Math.ceil(58 / step));

  const path = (s: Series) => {
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

  const lines = [
    { s: base, color: BASE_COLOR, opacity: 0.55, width: 2, label: baseLabel },
    { s: candidate, color: CANDIDATE_COLOR, opacity: 1, width: 2.25, label: candidateLabel },
  ];

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
          const v = min + ((max - min) * i) / ticks;
          return (
            <g key={i}>
              <line x1={pad.left} x2={width - pad.right} y1={y(v)} y2={y(v)} stroke="var(--chart-grid)" />
              <text x={pad.left - 10} y={y(v) + 4} fill="var(--muted)" fontSize="12" textAnchor="end">
                {yFmt(v)}
              </text>
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
          <g key={l.label} opacity={l.opacity}>
            <path d={path(l.s)} stroke={l.color} strokeWidth={l.width} fill="none" strokeLinejoin="round" strokeLinecap="round" />
            {step >= 10 &&
              l.s.map((v, i) =>
                v == null ? null : <circle key={i} cx={x(i)} cy={y(v)} r={3.5} fill="var(--background)" stroke={l.color} strokeWidth={1.5} />,
              )}
          </g>
        ))}

        {lines.map((l) => {
          const j = last(l.s);
          // The label would sit on the other line where it carries on right after.
          const crowded = lines.some((o) => o !== l && o.s.slice(j + 1, j + 4).some((v) => v != null));
          if (j < 0 || crowded) return null;
          return (
            <text
              key={`end-${l.label}`}
              x={x(j) + 8}
              y={y(l.s[j]!) + 4}
              fill={l.color === BASE_COLOR ? 'var(--muted)' : l.color}
              fontSize="12"
              fontWeight="600"
            >
              {yFmt(l.s[j]!)}
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
          const tx = Math.min(Math.max(x(hover), pad.left + 80), width - pad.right - 80);
          const rows = lines.map((l) => ({ ...l, v: l.s[hover] }));
          return (
            <g style={{ pointerEvents: 'none' }}>
              <line x1={x(hover)} x2={x(hover)} y1={pad.top} y2={pad.top + innerH} stroke="var(--foreground)" strokeOpacity="0.12" strokeDasharray="3 3" />
              <g transform={`translate(${tx}, ${pad.top})`}>
                <rect x="-80" y="0" width="160" height="64" rx="8" fill="var(--chart-tooltip-bg)" stroke="var(--border)" />
                <text x="-68" y="18" fill="var(--muted)" fontSize="12">{labels[hover]}</text>
                {rows.map((r, k) => (
                  <g key={r.label} fontSize="13">
                    <text x="-68" y={36 + k * 16} fill={r.color === BASE_COLOR ? 'var(--muted)' : r.color} fontFamily="var(--font-mono)">
                      {r.label.length > 12 ? `${r.label.slice(0, 11)}…` : r.label}
                    </text>
                    <text x="68" y={36 + k * 16} fill="var(--foreground)" textAnchor="end" fontWeight="500">
                      {r.v == null ? '—' : yFmt(r.v)}
                    </text>
                  </g>
                ))}
              </g>
            </g>
          );
        })()}
      </svg>
    </div>
  );
}
