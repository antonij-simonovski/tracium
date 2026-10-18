import type { ReactNode } from 'react';
import { CostBarChart, LatencyChart, Panel, PanelFrame, isLongRange } from '../../../common';
import type { CostPoint, LatencyPoint, ChartMarker } from '../../../common/interfaces';

interface ChartsRowProps {
  costSeries: CostPoint[];
  latSeries: LatencyPoint[];
  range?: string;
  costMarkers?: ChartMarker[];
}

function LegendItem({ swatch, children }: { swatch: ReactNode; children: ReactNode }) {
  return (
    <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
      {swatch}
      {children}
    </span>
  );
}

export function ChartsRow({ costSeries, latSeries, range, costMarkers }: ChartsRowProps) {
  return (
    <div style={{ borderBottom: '1px solid var(--border)', marginBottom: 44 }}>
      <PanelFrame
        columns="1fr 1fr"
        left={
          <Panel eyebrow="Spend" title={range === '24h' ? 'Hourly cost' : 'Daily cost'}>
            <CostBarChart series={costSeries} markers={costMarkers} />
          </Panel>
        }
        right={
          !isLongRange(range ?? '') && (
            <Panel
              eyebrow="Latency"
              title="Response time · p50 / p95 / p99"
              right={
                <>
                  <LegendItem swatch={<span style={{ width: 10, height: 2, background: 'var(--foreground)', opacity: 0.5 }} />}>
                    p50
                  </LegendItem>
                  <LegendItem swatch={<span style={{ width: 10, height: 2, background: 'var(--accent)' }} />}>p95</LegendItem>
                  <LegendItem swatch={<span style={{ width: 10, height: 2, borderTop: '2px dashed var(--warning)' }} />}>
                    p99
                  </LegendItem>
                </>
              }
            >
              <LatencyChart series={latSeries} />
            </Panel>
          )
        }
      />
    </div>
  );
}
