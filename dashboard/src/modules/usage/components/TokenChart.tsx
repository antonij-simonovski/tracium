// TokenChart — tokens per bucket as stacked bars, input below and output on top.
// Shares DailyChart's grid, axes and tooltip so the two charts read alike.

import { useState } from 'react';
import type { CSSProperties } from 'react';
import { fmtNum, fmtTokens } from '../../../common';
import type { TokenSeriesPoint } from '../interfaces';
import { niceCeil, tickIndices } from './DailyChart';
import chart from './DailyChart.module.css';
import styles from './TokenChart.module.css';

export function TokenChartLegend() {
  return (
    <div className={chart.legend}>
      <span className={chart.legendItem}>
        <span className={`${styles.swatch} ${styles.input}`} />
        Input
      </span>
      <span className={chart.legendItem}>
        <span className={`${styles.swatch} ${styles.output}`} />
        Output
      </span>
    </div>
  );
}

export function TokenChart({ series }: { series: TokenSeriesPoint[] }) {
  const [hoverIdx, setHoverIdx] = useState<number | null>(null);
  const niceMax = niceCeil(Math.max(0, ...series.map((d) => d.input + d.output)));
  const lastIdx = series.length - 1;

  return (
    <div className={chart.chart}>
      <div className={chart.grid}>
        {[0, 0.25, 0.5, 0.75, 1].map((t) => (
          <div
            key={t}
            className={`${chart.gridLine} ${t === 0 ? chart.base : ''}`}
            style={{ bottom: t * 100 + '%' }}
          />
        ))}
      </div>

      <div className={chart.yAxis}>
        <div className={chart.yTop}>{fmtTokens(niceMax)}</div>
        <div className={chart.yMid}>{fmtTokens(niceMax / 2)}</div>
        <div className={chart.yZero}>0</div>
      </div>

      <div className={chart.plot}>
        <div className={chart.bars}>
          {series.map((d, i) => {
            const total = d.input + d.output;
            const h = (total / niceMax) * 100;
            const outShare = total > 0 ? (d.output / total) * 100 : 0;
            return (
              <div
                key={i}
                className={`${chart.bar} ${styles.bar}`}
                onMouseEnter={() => setHoverIdx(i)}
                onMouseLeave={() => setHoverIdx(null)}
              >
                <div className={styles.stack} style={{ '--h': h + '%' } as CSSProperties}>
                  <div className={styles.outFill} style={{ '--share': outShare + '%' } as CSSProperties} />
                  <div className={styles.inFill} />
                </div>
                {hoverIdx === i && (
                  <div
                    className={chart.tooltip}
                    style={{ '--tip-bottom': `calc(${h}% + 14px)` } as CSSProperties}
                  >
                    <div className={chart.tipLabel}>{d.label}</div>
                    <div className={chart.tipCost}>{fmtTokens(total)} tokens</div>
                    <div className={chart.tipRuns}>
                      {fmtTokens(d.input)} in · {fmtTokens(d.output)} out
                    </div>
                    {d.runs > 0 && (
                      <div className={chart.tipRuns}>
                        {fmtNum(Math.round(total / d.runs))} per run
                      </div>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>

      <div className={chart.xAxis}>
        {tickIndices(series.length).map((idx) => (
          <span
            key={idx}
            className={`${chart.xTick} ${idx === 0 ? chart.start : idx === lastIdx ? chart.end : chart.mid}`}
          >
            {series[idx]?.label}
          </span>
        ))}
      </div>
    </div>
  );
}

export function TokenMix({ series }: { series: TokenSeriesPoint[] }) {
  const input = series.reduce((s, d) => s + d.input, 0);
  const output = series.reduce((s, d) => s + d.output, 0);
  const runs = series.reduce((s, d) => s + d.runs, 0);
  const total = input + output;
  const outShare = total > 0 ? (output / total) * 100 : 0;

  const rows = [
    { label: 'Input', value: fmtTokens(input), swatch: styles.input },
    { label: 'Output', value: fmtTokens(output), swatch: styles.output },
    { label: 'Output share', value: `${outShare.toFixed(0)}%` },
    { label: 'Per run', value: runs > 0 ? fmtNum(Math.round(total / runs)) : '—' },
  ];

  return (
    <div className={styles.mix}>
      <div className={styles.track}>
        <div className={styles.trackOut} style={{ '--share': outShare + '%' } as CSSProperties} />
      </div>
      {rows.map((r) => (
        <div key={r.label} className={styles.mixRow}>
          <span className={styles.mixLabel}>
            {r.swatch && <span className={`${styles.swatch} ${r.swatch}`} />}
            {r.label}
          </span>
          <span className={styles.mixValue}>{r.value}</span>
        </div>
      ))}
    </div>
  );
}
