import React, { useEffect, useRef, useState } from 'react';
import { EmptyState, useResize, bucketLabel, SEVERITY_META } from '../../../../common';
import type { Anomaly } from '../../interfaces';
import {
  METRIC_META,
  anomalyValue,
  anomalyKey,
  zLabel,
  ratioLabel,
  versionChangeLabel,
  bySeverity,
  groupIncidents,
  type Incident,
} from '../../utils/anomalies';

interface OutliersPanelProps {
  anomalies: Anomaly[]; // already filtered to non-dismissed
  range: string;
  dismissedCount?: number;
  // Controlled by the page so a chart flag can expand the matching row.
  selectedKey: string | null;
  onSelectKey: (key: string | null) => void;
  onDismiss: (a: Anomaly) => void;
  onRestoreAll?: () => void;
  onInspect?: (a: Anomaly) => void;
}

type ViewMode = 'list' | 'grouped';

// The list can run to dozens of rows; cap it and scroll inside so the panel
// stays a fixed, scannable size instead of pushing the page down.
const LIST_MAX_H = 460;

// The detection thresholds (σ) the API flags at, made visible as ticks on each
// row's magnitude bar. The ceiling clamps a single extreme outlier so ordinary
// ones don't collapse onto the floor; the exact z stays in the row.
const Z_TICKS = [3, 4.5, 6];
const Z_CEIL = 12;
const BASELINE_LABEL = '28-day';

export function OutliersPanel({
  anomalies,
  range,
  dismissedCount = 0,
  selectedKey,
  onSelectKey,
  onDismiss,
  onRestoreAll,
  onInspect,
}: OutliersPanelProps) {
  const ref = useRef<HTMLDivElement>(null);
  const width = useResize(ref);
  const narrow = width > 0 && width < 620;
  const [view, setView] = useState<ViewMode>('list');

  const sorted = [...anomalies].sort(bySeverity);
  const incidents = groupIncidents(anomalies);

  return (
    <div
      ref={ref}
      style={{
        background: 'var(--background)',
        border: '1px solid var(--border)',
        borderRadius: 14,
        padding: 28,
        display: 'flex',
        flexDirection: 'column',
        gap: 18,
        marginBottom: 44,
      }}
    >
      <Header
        count={sorted.length}
        incidentCount={incidents.length}
        view={view}
        onView={setView}
        narrow={narrow}
      />

      {sorted.length === 0 ? (
        <EmptyState
          message="No outliers in this window"
          description={`Nothing strayed far enough from its ${BASELINE_LABEL} baseline to flag. Cost, error-rate and run-volume are all within their usual range.`}
        />
      ) : view === 'list' ? (
        <div style={scrollArea}>
          {sorted.map((a) => {
            const expanded = anomalyKey(a) === selectedKey;
            return (
              <OutlierRow
                key={anomalyKey(a)}
                anomaly={a}
                range={range}
                narrow={narrow}
                expanded={expanded}
                onToggle={() => onSelectKey(expanded ? null : anomalyKey(a))}
                onDismiss={onDismiss}
                onInspect={onInspect}
              />
            );
          })}
        </div>
      ) : (
        <div style={scrollArea}>
          {incidents.map((inc) => {
            const expanded = inc.anomalies.some((a) => anomalyKey(a) === selectedKey);
            return (
              <IncidentCard
                key={inc.key}
                incident={inc}
                range={range}
                narrow={narrow}
                expanded={expanded}
                onToggle={() => onSelectKey(expanded ? null : anomalyKey(inc.anomalies[0]))}
                onDismiss={onDismiss}
                onInspect={onInspect}
              />
            );
          })}
        </div>
      )}

      {dismissedCount > 0 && onRestoreAll && (
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, paddingTop: 12, borderTop: '1px solid var(--border)' }}>
          <span style={{ fontSize: 13, color: 'var(--muted)' }}>{dismissedCount} dismissed this session</span>
          <button onClick={onRestoreAll} style={linkBtn}>
            Undo all
          </button>
        </div>
      )}
    </div>
  );
}

function Header({
  count,
  incidentCount,
  view,
  onView,
  narrow,
}: {
  count: number;
  incidentCount: number;
  view: ViewMode;
  onView: (v: ViewMode) => void;
  narrow: boolean;
}) {
  const grouped = view === 'grouped';
  return (
    <div style={{ display: 'flex', alignItems: 'flex-end', justifyContent: 'space-between', gap: 20, flexWrap: 'wrap' }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        <span style={{ fontSize: 12, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--muted)' }}>
          Outliers · {grouped ? 'incidents' : 'triage'}
        </span>
        <span style={{ fontSize: 18, fontWeight: 600, letterSpacing: '-0.01em', color: 'var(--foreground)' }}>
          {grouped
            ? `${incidentCount} incident${incidentCount === 1 ? '' : 's'}`
            : `${count} beyond baseline`}
        </span>
        <span style={{ fontSize: 14, color: 'var(--muted)' }}>
          {grouped
            ? `Flags for the same target on the same day, grouped. Most severe first.`
            : `Each row is a flagged day, most severe first. Distance from the ${BASELINE_LABEL} baseline is shown per row.`}
        </span>
      </div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 16, flexWrap: 'wrap' }}>
        {!narrow && <Legend />}
        <ViewToggle view={view} onView={onView} />
      </div>
    </div>
  );
}

function Legend() {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 12, fontSize: 13, color: 'var(--muted)' }}>
      {(['critical', 'warning', 'info'] as const).map((s) => (
        <span key={s} style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
          <SeverityDot severity={s} />
          {s}
        </span>
      ))}
    </div>
  );
}

function ViewToggle({ view, onView }: { view: ViewMode; onView: (v: ViewMode) => void }) {
  const opt = (v: ViewMode, label: string) => {
    const active = view === v;
    return (
      <button
        key={v}
        onClick={() => onView(v)}
        style={{
          fontSize: 13,
          fontWeight: active ? 600 : 500,
          padding: '5px 12px',
          borderRadius: 7,
          border: 'none',
          background: active ? 'var(--background)' : 'transparent',
          color: active ? 'var(--foreground)' : 'var(--muted)',
          boxShadow: active ? '0 1px 2px color-mix(in srgb, var(--foreground) 12%, transparent)' : 'none',
          cursor: 'pointer',
        }}
      >
        {label}
      </button>
    );
  };
  return (
    <div style={{ display: 'flex', gap: 2, padding: 3, borderRadius: 9, background: 'var(--surface)', border: '1px solid var(--border)' }}>
      {opt('list', 'List')}
      {opt('grouped', 'Grouped')}
    </div>
  );
}

function OutlierRow({
  anomaly: a,
  range,
  narrow,
  expanded,
  onToggle,
  onDismiss,
  onInspect,
}: {
  anomaly: Anomaly;
  range: string;
  narrow: boolean;
  expanded: boolean;
  onToggle: () => void;
  onDismiss: (a: Anomaly) => void;
  onInspect?: (a: Anomaly) => void;
}) {
  const rowRef = useScrollIntoView<HTMLDivElement>(expanded);
  const meta = SEVERITY_META[a.severity];
  const m = METRIC_META[a.metric];
  const target = a.scope === 'workflow' ? a.workflow : 'Workspace-wide';
  const ratio = ratioLabel(a.observed, a.expected);

  return (
    <div
      ref={rowRef}
      style={{
        flex: '0 0 auto', // don't let the scroll container shrink rows to fit
        border: `1px solid ${expanded ? meta.border : 'var(--border)'}`,
        borderRadius: 10,
        background: expanded ? meta.tint : 'var(--surface)',
        overflow: 'hidden',
        transition: 'background 120ms, border-color 120ms',
      }}
    >
      <button
        onClick={onToggle}
        aria-expanded={expanded}
        style={{
          width: '100%',
          display: 'flex',
          alignItems: 'center',
          gap: 14,
          padding: '11px 15px',
          background: 'transparent',
          border: 'none',
          cursor: 'pointer',
          textAlign: 'left',
          flexWrap: narrow ? 'wrap' : 'nowrap',
        }}
      >
        <SeverityDot severity={a.severity} size={a.severity === 'critical' ? 11 : 9} />

        <div style={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 150, flex: narrow ? '1 1 100%' : '0 0 auto' }}>
          <span style={{ fontSize: 14, fontWeight: 600, color: 'var(--foreground)' }}>
            {m.kind} {a.direction}
          </span>
          <span style={{ fontFamily: MONO, fontSize: 13, color: 'var(--muted)' }}>
            {target} · {bucketLabel(a.bucket_ms, range)}
          </span>
          <VersionChangeNote anomaly={a} range={range} />
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 120, flex: narrow ? '1 1 auto' : '0 0 auto', textAlign: narrow ? 'left' : 'right', marginLeft: narrow ? 0 : 'auto' }}>
          <span style={{ fontSize: 16, fontWeight: 600, fontVariantNumeric: 'tabular-nums', color: 'var(--foreground)' }}>
            {anomalyValue(a.metric, a.observed)}
          </span>
          <span style={{ fontSize: 12, color: 'var(--muted)', fontVariantNumeric: 'tabular-nums' }}>
            from {anomalyValue(a.metric, a.expected)}
            {ratio && <> · <span style={{ color: meta.color, fontWeight: 600 }}>{ratio}</span></>}
          </span>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: 12, flex: narrow ? '1 1 100%' : '0 0 auto', minWidth: narrow ? 0 : 150 }}>
          <MagnitudeBar score={a.score} color={meta.color} />
          <span style={{ fontFamily: MONO, fontSize: 13, fontWeight: 600, color: meta.color, fontVariantNumeric: 'tabular-nums', width: 44, textAlign: 'right' }}>
            {zLabel(a.score)}
          </span>
          <Chevron open={expanded} />
        </div>
      </button>

      {expanded && (
        <div style={{ padding: '4px 16px 16px', borderTop: `1px solid ${meta.border}` }}>
          <div style={{ display: 'flex', gap: 20, flexWrap: 'wrap', alignItems: 'flex-start', paddingTop: 14 }}>
            <WhyFlagged anomaly={a} />
            <div style={{ display: 'flex', flexDirection: 'column', gap: 10, flex: '1 1 220px', minWidth: 200 }}>
              <span style={{ fontSize: 14, color: 'var(--muted)', lineHeight: 1.5 }}>{a.summary}</span>
              <RowActions anomaly={a} onDismiss={onDismiss} onInspect={onInspect} />
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function IncidentCard({
  incident,
  range,
  narrow,
  expanded,
  onToggle,
  onDismiss,
  onInspect,
}: {
  incident: Incident;
  range: string;
  narrow: boolean;
  expanded: boolean;
  onToggle: () => void;
  onDismiss: (a: Anomaly) => void;
  onInspect?: (a: Anomaly) => void;
}) {
  const cardRef = useScrollIntoView<HTMLDivElement>(expanded);
  const meta = SEVERITY_META[incident.severity];
  const target = incident.scope === 'workflow' ? incident.workflow : 'Workspace-wide';
  const n = incident.anomalies.length;
  const workflowAnom = incident.anomalies.find((a) => a.scope === 'workflow');
  const versionAnom = incident.anomalies.find((a) => a.version_change);

  return (
    <div
      ref={cardRef}
      style={{
        flex: '0 0 auto', // don't let the scroll container shrink cards to fit
        border: `1px solid ${expanded ? meta.border : 'var(--border)'}`,
        borderLeft: `3px solid ${meta.color}`,
        borderRadius: 10,
        background: expanded ? meta.tint : 'var(--surface)',
        overflow: 'hidden',
      }}
    >
      <button
        onClick={onToggle}
        aria-expanded={expanded}
        style={{
          width: '100%',
          display: 'flex',
          alignItems: 'center',
          gap: 14,
          padding: '11px 15px',
          background: 'transparent',
          border: 'none',
          cursor: 'pointer',
          textAlign: 'left',
          flexWrap: 'wrap',
        }}
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 2, flex: '1 1 auto', minWidth: 160 }}>
          <span style={{ fontSize: 15, fontWeight: 600, color: 'var(--foreground)' }}>{target}</span>
          <span style={{ fontFamily: MONO, fontSize: 13, color: 'var(--muted)' }}>{bucketLabel(incident.bucket_ms, range)}</span>
          {versionAnom && <VersionChangeNote anomaly={versionAnom} range={range} />}
        </div>

        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', flex: narrow ? '1 1 100%' : '0 1 auto' }}>
          {incident.anomalies.map((a) => (
            <MetricChip key={anomalyKey(a)} anomaly={a} />
          ))}
        </div>

        <span style={{ fontSize: 13, color: 'var(--muted)', marginLeft: narrow ? 0 : 'auto' }}>
          {n} flag{n === 1 ? '' : 's'}
        </span>
        <Chevron open={expanded} />
      </button>

      {expanded && (
        <div style={{ padding: '0 16px 16px', display: 'flex', flexDirection: 'column', gap: 12 }}>
          {incident.anomalies.map((a) => (
            <div key={anomalyKey(a)} style={{ display: 'flex', gap: 20, flexWrap: 'wrap', alignItems: 'flex-start', paddingTop: 14, borderTop: `1px solid ${meta.border}` }}>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8, flex: '0 0 auto' }}>
                <span style={{ fontSize: 13, fontWeight: 600, color: SEVERITY_META[a.severity].color }}>
                  {METRIC_META[a.metric].kind} {a.direction}
                </span>
                <WhyFlagged anomaly={a} />
              </div>
              <span style={{ fontSize: 14, color: 'var(--muted)', lineHeight: 1.5, flex: '1 1 200px', minWidth: 180 }}>{a.summary}</span>
            </div>
          ))}
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', paddingTop: 2 }}>
            {workflowAnom && onInspect && (
              <button onClick={() => onInspect(workflowAnom)} style={primaryBtn}>
                Inspect {workflowAnom.workflow} →
              </button>
            )}
            <button onClick={() => incident.anomalies.forEach(onDismiss)} style={ghostBtn}>
              Dismiss {n === 1 ? 'flag' : 'all'}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

const MONO = 'ui-monospace, SFMono-Regular, Menlo, monospace';

// The capped, scrolling list body. A small gutter keeps the scrollbar off the
// row borders; the negative-margin trick isn't needed since rows sit inset.
const scrollArea: React.CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 8,
  maxHeight: LIST_MAX_H,
  overflowY: 'auto',
  paddingRight: 6,
  scrollbarGutter: 'stable',
};

function SeverityDot({ severity, size = 8 }: { severity: Anomaly['severity']; size?: number }) {
  const meta = SEVERITY_META[severity];
  return (
    <span
      style={{
        width: size,
        height: size,
        borderRadius: '50%',
        background: meta.color,
        flex: 'none',
        boxShadow: severity === 'critical' ? `0 0 8px ${meta.border}` : 'none',
      }}
    />
  );
}

function VersionChangeNote({ anomaly: a, range }: { anomaly: Anomaly; range: string }) {
  if (!a.version_change) return null;
  return (
    <span
      title={`First seen ${bucketLabel(a.version_change.first_seen_ms, range)}`}
      style={{ fontFamily: MONO, fontSize: 12, color: 'var(--accent)' }}
    >
      {versionChangeLabel(a)}
    </span>
  );
}

// MagnitudeBar shows |z| on a 0→ceiling scale with ticks at the detection
// thresholds, so how far past the cutoff a flag sits is legible at a glance.
function MagnitudeBar({ score, color }: { score: number; color: string }) {
  const z = Math.min(Math.abs(score), Z_CEIL);
  return (
    <div style={{ position: 'relative', flex: 1, minWidth: 60, height: 6, borderRadius: 3, background: 'var(--surface-active)', overflow: 'hidden' }}>
      <div style={{ position: 'absolute', left: 0, top: 0, bottom: 0, width: `${(z / Z_CEIL) * 100}%`, background: color, borderRadius: 3 }} />
      {Z_TICKS.map((t) => (
        <span key={t} style={{ position: 'absolute', top: 0, bottom: 0, left: `${(t / Z_CEIL) * 100}%`, width: 1, background: 'color-mix(in srgb, var(--foreground) 22%, transparent)' }} />
      ))}
    </div>
  );
}

// MetricChip is the compact per-metric summary shown on a collapsed incident.
function MetricChip({ anomaly: a }: { anomaly: Anomaly }) {
  const meta = SEVERITY_META[a.severity];
  const ratio = ratioLabel(a.observed, a.expected);
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'baseline',
        gap: 5,
        fontSize: 12,
        padding: '4px 9px',
        borderRadius: 20,
        background: meta.tint,
        border: `1px solid ${meta.border}`,
        color: meta.color,
        whiteSpace: 'nowrap',
        fontVariantNumeric: 'tabular-nums',
      }}
    >
      <span style={{ fontWeight: 600 }}>{METRIC_META[a.metric].kind}</span>
      <span>{anomalyValue(a.metric, a.observed)}</span>
      {ratio && <span style={{ opacity: 0.85 }}>{ratio}</span>}
    </span>
  );
}

function WhyFlagged({ anomaly: a }: { anomaly: Anomaly }) {
  const meta = SEVERITY_META[a.severity];
  const signedDev = `${a.deviation >= 0 ? '+' : '−'}${anomalyValue(a.metric, Math.abs(a.deviation))}`;
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 7, padding: 12, borderRadius: 8, background: 'var(--surface-alt)', minWidth: 200 }}>
      <span style={{ fontSize: 12, letterSpacing: '0.1em', textTransform: 'uppercase', color: 'var(--muted)' }}>Why flagged</span>
      <DetailStat k="observed" v={anomalyValue(a.metric, a.observed)} color={meta.color} />
      <DetailStat k="baseline" v={anomalyValue(a.metric, a.expected)} />
      <DetailStat k="deviation" v={signedDev} />
      <DetailStat k="z-score" v={zLabel(a.score)} />
    </div>
  );
}

function DetailStat({ k, v, color }: { k: string; v: string; color?: string }) {
  return (
    <div style={{ display: 'flex', justifyContent: 'space-between', gap: 16, fontSize: 13 }}>
      <span style={{ color: 'var(--muted)' }}>{k}</span>
      <span style={{ fontFamily: MONO, fontVariantNumeric: 'tabular-nums', color: color ?? 'var(--foreground)' }}>{v}</span>
    </div>
  );
}

function RowActions({ anomaly: a, onDismiss, onInspect }: { anomaly: Anomaly; onDismiss: (a: Anomaly) => void; onInspect?: (a: Anomaly) => void }) {
  return (
    <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginTop: 'auto' }}>
      {a.scope === 'workflow' && onInspect && (
        <button onClick={() => onInspect(a)} style={primaryBtn}>
          Inspect workflow →
        </button>
      )}
      <button onClick={() => onDismiss(a)} style={ghostBtn}>
        Not an outlier
      </button>
    </div>
  );
}

function Chevron({ open }: { open: boolean }) {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="var(--muted)"
      strokeWidth="2.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      style={{ flex: 'none', transform: open ? 'rotate(180deg)' : 'none', transition: 'transform 140ms' }}
    >
      <polyline points="6 9 12 15 18 9" />
    </svg>
  );
}

// useScrollIntoView brings a row into view when it becomes the expanded one via
// an external selection (e.g. a chart flag was clicked), not on first mount.
function useScrollIntoView<T extends HTMLElement>(active: boolean) {
  const ref = useRef<T>(null);
  const mounted = useRef(false);
  useEffect(() => {
    if (active && mounted.current) {
      ref.current?.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    }
    mounted.current = true;
  }, [active]);
  return ref;
}

const primaryBtn: React.CSSProperties = {
  fontSize: 13,
  padding: '8px 14px',
  borderRadius: 8,
  border: '1px solid var(--accent-border)',
  background: 'var(--accent-soft)',
  color: 'var(--accent)',
  cursor: 'pointer',
};

const ghostBtn: React.CSSProperties = {
  fontSize: 13,
  padding: '8px 14px',
  borderRadius: 8,
  border: '1px solid var(--border-strong)',
  background: 'transparent',
  color: 'var(--muted)',
  cursor: 'pointer',
};

const linkBtn: React.CSSProperties = {
  fontSize: 13,
  color: 'var(--muted)',
  background: 'transparent',
  border: 'none',
  cursor: 'pointer',
  padding: 0,
};
