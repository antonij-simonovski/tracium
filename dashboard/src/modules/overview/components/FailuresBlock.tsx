import { HorizonStrip, IconArrowRight, fmtNum } from '../../../common';
import type { ErrorPoint, ChartMarker } from '../../../common/interfaces';
import { SectionRule } from './SectionRule';

interface FailuresBlockProps {
  series?: ErrorPoint[];
  totalFailed: number;
  worstWorkflow: string;
  onViewWorkflows: () => void;
  range?: string;
  errorMarkers?: ChartMarker[];
}

function SummaryStat({ label, value, valueColor }: { label: string; value: string; valueColor?: string }) {
  return (
    <div>
      <span style={{ color: 'var(--muted)' }}>{label} </span>
      <span style={{ color: valueColor ?? 'var(--foreground)', fontWeight: valueColor ? 500 : 400, fontVariantNumeric: 'tabular-nums' }}>
        {value}
      </span>
    </div>
  );
}

function Divider() {
  return <div style={{ width: 1, height: 14, background: 'var(--border)' }} />;
}

export function FailuresBlock({ series, totalFailed, worstWorkflow, onViewWorkflows, range, errorMarkers }: FailuresBlockProps) {
  const hourly = range === '24h';
  return (
    <div style={{ marginBottom: 44 }}>
      <SectionRule
        eyebrow="Reliability"
        title={hourly ? 'Failures by hour' : 'Failures by day'}
        subtitle={`Error count per ${hourly ? 'hour' : 'day'} across all workflows · hover for rate`}
        right={
          <button
            onClick={onViewWorkflows}
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 5,
              fontSize: 13,
              fontWeight: 500,
              color: 'var(--foreground)',
              padding: '5px 10px',
              border: '1px solid var(--border)',
              borderRadius: 7,
              background: 'transparent',
            }}
          >
            View workflows <IconArrowRight size={12} />
          </button>
        }
      />
      {series && series.length > 0 && <HorizonStrip data={series} markers={errorMarkers} />}
      <div style={{ display: 'flex', gap: 36, alignItems: 'center', marginTop: series && series.length > 0 ? 20 : 0, fontSize: 13.5 }}>
        <SummaryStat label="Total failed" value={fmtNum(totalFailed)} valueColor="var(--error)" />
        <Divider />
        <SummaryStat label="Worst workflow" value={worstWorkflow} />
      </div>
    </div>
  );
}
