import { SeverityTag, plural } from '../../../common';
import hover from '../../../common/styles/hover.module.css';
import type { Incident } from '../interfaces';
import { signalsOf, sinceLabel, subjectLabel } from '../utils/incidents';

interface IncidentRowProps {
  incident: Incident;
  onOpen: (id: string) => void;
  onOpenSubject: (inc: Incident) => void;
}

export function IncidentRow({ incident: inc, onOpen, onOpenSubject }: IncidentRowProps) {
  const linked = inc.scope === 'workflow' || inc.scope === 'client';
  const n = signalsOf(inc).length;
  return (
    <div
      className={hover.row}
      style={{
        display: 'flex',
        alignItems: 'center',
        flexWrap: 'wrap',
        gap: '6px 16px',
        padding: '14px 6px',
        borderBottom: '1px solid color-mix(in srgb, var(--border) 55%, transparent)',
      }}
    >
      <SeverityTag severity={inc.severity} />
      <button
        onClick={() => onOpen(inc.id)}
        style={{ flex: '1 1 260px', minWidth: 0, background: 'none', border: 'none', padding: 0, textAlign: 'left', font: 'inherit', fontSize: 14.5, fontWeight: 500, color: 'var(--foreground)', cursor: 'pointer', overflowWrap: 'anywhere' }}
      >
        {inc.title}
      </button>
      {linked ? (
        <button onClick={() => onOpenSubject(inc)} className={hover.text} style={{ background: 'none', border: 'none', padding: 0, font: 'inherit', fontSize: 13, cursor: 'pointer', fontFamily: 'var(--font-mono)' }}>
          {subjectLabel(inc)}
        </button>
      ) : (
        <span style={{ fontSize: 13, color: 'var(--muted)' }}>{subjectLabel(inc)}</span>
      )}
      <span style={{ fontSize: 13, color: 'var(--muted)' }}>{plural(n, 'signal')}</span>
      <span style={{ fontSize: 13, color: 'var(--muted)' }}>
        {inc.status === 'resolved' && inc.resolved_at ? `resolved ${sinceLabel(inc.resolved_at)}` : `opened ${sinceLabel(inc.opened_at)}`}
      </span>
      {inc.version_change && (
        <span style={{ fontSize: 13, color: 'var(--accent)', fontFamily: 'var(--font-mono)' }}>likely cause: {inc.version_change.version}</span>
      )}
    </div>
  );
}
