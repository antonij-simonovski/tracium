// DailyChart — full-width cost-per-bucket bar chart with a runs line overlaid on
// a second (hidden) scale, so cost and volume read at a glance. A hover tooltip
// gives the exact cost + runs per bucket. Bucket count is data-driven, so it
// works for 24h/7d/30d ranges.

import { useState } from 'react';
import type { CSSProperties } from 'react';
import { fmtCost, fmtNum } from '../../../common';
import type { DailySeriesPoint } from '../interfaces';
import { niceCeil, tickIndices } from '../utils';
import styles from './DailyChart.module.css';

// DailyChartLegend — the Cost / Runs key, rendered in the panel header so it
// clarifies the two dimensions without a hover.
export function DailyChartLegend() {
  return (
    <div className={styles.legend}>
      <span className={styles.legendItem}>
        <span className={styles.legendBar} />
        Cost
      </span>
      <span className={styles.legendItem}>
        <span className={styles.legendLine} />
        Runs
      </span>
    </div>
  );
}

export function DailyChart({ series }: { series: DailySeriesPoint[] }) {
  const [hoverIdx, setHoverIdx] = useState<number | null>(null);
  const niceMax = niceCeil(Math.max(0, ...series.map((d) => d.cost)));
  // Runs ride a separate hidden scale so the line shares the plot with the bars.
  const runsMax = niceCeil(Math.max(1, ...series.map((d) => d.runs)));
  const lastIdx = series.length - 1;

  // Runs polyline in a normalised 0..N × 0..100 viewBox (y inverted).
  const runsPoints = series
    .map((d, i) => `${i + 0.5},${100 - (d.runs / runsMax) * 100}`)
    .join(' ');

  return (
    <div className={styles.chart}>
      <div className={styles.grid}>
        {[0, 0.25, 0.5, 0.75, 1].map((t) => (
          <div
            key={t}
            className={`${styles.gridLine} ${t === 0 ? styles.base : ''}`}
            style={{ bottom: t * 100 + '%' }}
          />
        ))}
      </div>

      <div className={styles.yAxis}>
        <div className={styles.yTop}>{fmtCost(niceMax)}</div>
        <div className={styles.yMid}>{fmtCost(niceMax / 2)}</div>
        <div className={styles.yZero}>$0</div>
      </div>

      <div className={styles.plot}>
        <div className={styles.bars}>
          {series.map((d, i) => {
            const h = (d.cost / niceMax) * 100;
            return (
              <div
                key={i}
                className={styles.bar}
                onMouseEnter={() => setHoverIdx(i)}
                onMouseLeave={() => setHoverIdx(null)}
              >
                <div className={styles.barFill} style={{ '--h': h + '%' } as CSSProperties} />
                {hoverIdx === i && (
                  <div
                    className={styles.tooltip}
                    style={{ '--tip-bottom': `calc(${h}% + 14px)` } as CSSProperties}
                  >
                    <div className={styles.tipLabel}>{d.label}</div>
                    <div className={styles.tipCost}>{fmtCost(d.cost)}</div>
                    <div className={styles.tipRuns}>{fmtNum(d.runs)} runs</div>
                  </div>
                )}
              </div>
            );
          })}
        </div>
        {series.length > 1 && (
          <svg
            className={styles.runsOverlay}
            viewBox={`0 0 ${series.length} 100`}
            preserveAspectRatio="none"
          >
            <polyline className={styles.runsLine} points={runsPoints} vectorEffect="non-scaling-stroke" />
          </svg>
        )}
      </div>

      <div className={styles.xAxis}>
        {tickIndices(series.length).map((idx) => (
          <span
            key={idx}
            className={`${styles.xTick} ${idx === 0 ? styles.start : idx === lastIdx ? styles.end : styles.mid}`}
          >
            {series[idx]?.label}
          </span>
        ))}
      </div>
    </div>
  );
}
