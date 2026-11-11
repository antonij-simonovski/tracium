import { useState } from 'react';
import { SEVERITY_META } from '../../../common';
import { useIncidentBacktest } from '../hooks/useMetrics';
import type { AnomalyMetric, AnomalySeverity } from '../interfaces';
import { METRIC_META } from '../utils/anomalies';

const linkStyle = {
  padding: 0,
  border: 'none',
  background: 'transparent',
  color: 'var(--foreground)',
  fontFamily: 'inherit',
  fontSize: 'inherit',
  textDecoration: 'underline',
  cursor: 'pointer',
} as const;

const SEVERITIES: AnomalySeverity[] = ['critical', 'warning', 'info'];

const fmtWhen = (iso: string) =>
  new Date(iso).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });

// What the incident evaluator would have opened over the last 30 days.
export function BacktestCard({ onSetUpAlerts }: { onSetUpAlerts?: () => void }) {
  const [asked, setAsked] = useState(false);
  const [open, setOpen] = useState(false);
  const backtest = useIncidentBacktest('30d', asked);
  const data = backtest.data;

  let line: React.ReactNode;
  let action: React.ReactNode = null;
  if (!asked) {
    line = 'See what Tracium would have alerted you about in the last 30 days';
    action = <button style={linkStyle} onClick={() => setAsked(true)}>Check</button>;
  } else if (backtest.isLoading) {
    line = <span role="status">Replaying the last 30 days of detection…</span>;
  } else if (backtest.isError || !data) {
    line = 'Could not replay the last 30 days.';
    action = <button style={linkStyle} onClick={() => backtest.refetch()}>Try again</button>;
  } else if (data.incidents === 0) {
    line = 'Nothing would have fired in the last 30 days.';
    action = onSetUpAlerts && <button style={linkStyle} onClick={onSetUpAlerts}>Set up alerts</button>;
  } else {
    const severities = SEVERITIES.filter((s) => data.by_severity[s]).map((s) => `${data.by_severity[s]} ${s}`);
    const metrics = Object.entries(data.by_metric)
      .sort((a, b) => (b[1] ?? 0) - (a[1] ?? 0))
      .map(([m, n]) => `${METRIC_META[m as AnomalyMetric]?.kind ?? m} ${n}`);
    line = (
      <>
        In the last 30 days Tracium would have alerted you {data.incidents} time{data.incidents === 1 ? '' : 's'}
        <span style={{ color: 'var(--muted-foreground)' }}> · {severities.join(', ')} · {metrics.join(', ')}</span>
      </>
    );
    action = (
      <>
        <button style={linkStyle} aria-expanded={open} onClick={() => setOpen((o) => !o)}>{open ? 'Hide' : 'See them'}</button>
        {onSetUpAlerts && <button style={linkStyle} onClick={onSetUpAlerts}>Set up alerts</button>}
      </>
    );
  }

  return (
    <div style={{ marginBottom: 28, border: '1px solid var(--border)', borderRadius: 10, background: 'var(--surface)' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap', padding: '10px 14px', fontSize: 13.5 }}>
        <span style={{ flex: 1, minWidth: 0, color: 'var(--foreground)' }}>{line}</span>
        {action}
      </div>
      {open && data && data.items.length > 0 && (
        <ul style={{ listStyle: 'none', margin: 0, padding: '0 14px 14px', display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13 }}>
          {data.items.map((it, i) => (
            <li key={i} style={{ display: 'flex', alignItems: 'baseline', gap: 10, flexWrap: 'wrap' }}>
              <span
                title={it.severity}
                style={{ width: 8, height: 8, borderRadius: '50%', background: SEVERITY_META[it.severity].color, flexShrink: 0 }}
              />
              <span style={{ flex: 1, minWidth: 0, overflowWrap: 'anywhere', color: 'var(--foreground)' }}>{it.title}</span>
              <span style={{ color: 'var(--muted-foreground)', fontVariantNumeric: 'tabular-nums' }}>
                {fmtWhen(it.opened_at)}
                {it.resolved_at ? ` – ${fmtWhen(it.resolved_at)}` : ' · still open'}
              </span>
            </li>
          ))}
          {data.incidents > data.items.length && (
            <li style={{ color: 'var(--muted-foreground)' }}>and {data.incidents - data.items.length} more</li>
          )}
        </ul>
      )}
    </div>
  );
}
