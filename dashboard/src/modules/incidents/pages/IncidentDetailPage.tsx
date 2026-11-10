import { Centered, SEVERITY_META, SeverityTag, Spinner, formatDateTime, type Severity } from '../../../common';
import hover from '../../../common/styles/hover.module.css';
import { METRIC_META, anomalyTime, anomalyValue } from '../../overview/utils/anomalies';
import { LikelyCause } from '../components/LikelyCause';
import { MuteMenu } from '../components/MuteMenu';
import { useIncident, useIncidentActions } from '../hooks/useIncidents';
import { groupByMetric, incidentFlag, incidentRange, sampleTraceIds, signalsOf, subjectLabel } from '../utils/incidents';
import { openSubject } from '../utils/navigation';

interface IncidentDetailPageProps {
  incidentId: string;
  setView: (v: string) => void;
  setSelected: (updater: (prev: Record<string, string>) => Record<string, string>) => void;
  setRange: (r: string) => void;
}

const label = { fontSize: 12, letterSpacing: '0.1em', textTransform: 'uppercase', color: 'var(--muted)', margin: '0 0 10px' } as const;
const linkBtn = { background: 'none', border: 'none', padding: 0, font: 'inherit', fontSize: 13.5, cursor: 'pointer' } as const;

export function IncidentDetailPage({ incidentId, setView, setSelected, setRange }: IncidentDetailPageProps) {
  const { data: inc, isLoading } = useIncident(incidentId);
  const { resolve, mute } = useIncidentActions(incidentId);

  if (isLoading) return <Centered><Spinner /></Centered>;
  if (!inc) return <Centered>Incident not found</Centered>;

  const signals = signalsOf(inc);
  const top = [...signals].sort((a, b) => SEVERITY_META[b.severity].rank - SEVERITY_META[a.severity].rank)[0];
  const traces = sampleTraceIds(inc);
  const flag = incidentFlag(inc);
  const busy = resolve.isPending || mute.isPending;
  const open = inc.status === 'open';

  const openTrace = (id: string) => {
    setSelected((s) => ({ ...s, traceId: id }));
    setView('trace');
  };
  const seeRuns = () => {
    setRange(incidentRange(inc));
    setSelected((s) => ({ ...s, workflow: inc.subject, runsFlag: flag ?? '' }));
    setView('workflows');
  };

  return (
    <div style={{ padding: 'clamp(20px, 4vw, 32px) clamp(16px, 4vw, 40px) 64px', maxWidth: 1000, margin: '0 auto', display: 'flex', flexDirection: 'column', gap: 28 }}>
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'flex-start', justifyContent: 'space-between', gap: 16 }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8, flex: '1 1 320px', minWidth: 0 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
            <SeverityTag severity={inc.severity as Severity} />
            <span style={{ fontSize: 12.5, fontWeight: 500, color: open ? 'var(--foreground)' : 'var(--muted)' }}>{open ? 'Open' : 'Resolved'}</span>
          </div>
          <h1 style={{ fontSize: 22, fontWeight: 600, letterSpacing: '-0.02em', margin: 0, overflowWrap: 'anywhere' }}>{inc.title}</h1>
          <p style={{ fontSize: 13.5, color: 'var(--muted)', margin: 0 }}>
            {inc.scope === 'workflow' || inc.scope === 'client' ? (
              <button onClick={() => openSubject(inc, setView, setSelected)} className={hover.text} style={{ ...linkBtn, fontFamily: 'var(--font-mono)' }}>
                {subjectLabel(inc)}
              </button>
            ) : subjectLabel(inc)}
            {' · '}opened {formatDateTime(Date.parse(inc.opened_at))}
            {inc.resolved_at && <> · resolved {formatDateTime(Date.parse(inc.resolved_at))}</>}
          </p>
        </div>
        {open && (
          <div style={{ display: 'flex', gap: 8 }}>
            <button
              onClick={() => resolve.mutate()}
              disabled={busy}
              style={{ padding: '8px 14px', borderRadius: 8, border: '1px solid var(--accent-border)', background: 'var(--accent-soft)', color: 'var(--accent)', font: 'inherit', fontSize: 13.5, cursor: 'pointer' }}
            >
              Resolve
            </button>
            {top && <MuteMenu metric={top.metric} metricLabel={METRIC_META[top.metric]?.noun ?? top.metric} disabled={busy} onMute={(input) => mute.mutate(input)} />}
          </div>
        )}
      </div>
      {(resolve.isError || mute.isError) && <p role="alert" style={{ color: 'var(--error)', fontSize: 13.5, margin: 0 }}>Could not update the incident. Try again.</p>}

      {inc.version_change && (
        <LikelyCause
          change={inc.version_change}
          firstSignalMs={Math.min(...signals.map((s) => s.bucket_ms))}
          onCompare={(workflow, base, candidate) => {
            setSelected((s) => ({ ...s, workflow, base, candidate }));
            setView('compare');
          }}
          onOpenWorkflow={(workflow) => openSubject({ scope: 'workflow', subject: workflow }, setView, setSelected)}
        />
      )}

      <section>
        <h2 style={label}>Signals</h2>
        {groupByMetric(signals).map(([metric, group]) => (
          <div key={metric} style={{ padding: '12px 0', borderTop: '1px solid var(--border)' }}>
            <div style={{ fontSize: 14.5, fontWeight: 600, marginBottom: 8 }}>{METRIC_META[metric]?.noun ?? metric}</div>
            {group.map((s) => (
              <div key={s.bucket_ms} style={{ display: 'flex', flexWrap: 'wrap', gap: '4px 16px', alignItems: 'baseline', padding: '4px 0' }}>
                <SeverityTag severity={s.severity} compact />
                <span style={{ fontSize: 14, flex: '1 1 300px', lineHeight: 1.5 }}>{s.summary}</span>
                <span style={{ fontSize: 13, fontVariantNumeric: 'tabular-nums' }}>
                  {anomalyValue(s.metric, s.observed)} <span style={{ color: 'var(--muted)' }}>vs {anomalyValue(s.metric, s.expected)}</span>
                </span>
                <span style={{ fontSize: 12.5, color: 'var(--muted)', fontFamily: 'var(--font-mono)' }}>{anomalyTime(s, '7d')}</span>
              </div>
            ))}
          </div>
        ))}
      </section>

      <section>
        <h2 style={label}>Example runs</h2>
        {traces.length === 0 ? (
          <p style={{ fontSize: 13.5, color: 'var(--muted)', margin: 0 }}>No example runs for these signals.</p>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
            {traces.map((id) => (
              <button key={id} onClick={() => openTrace(id)} className={hover.text} style={{ ...linkBtn, textAlign: 'left', fontFamily: 'var(--font-mono)', overflowWrap: 'anywhere' }}>
                {id} →
              </button>
            ))}
          </div>
        )}
        {inc.scope === 'workflow' && (
          <button onClick={seeRuns} className={hover.text} style={{ ...linkBtn, marginTop: 12, textDecoration: 'underline' }}>
            {flag ? 'See all flagged runs' : `See ${inc.subject} runs`}
          </button>
        )}
      </section>
    </div>
  );
}
