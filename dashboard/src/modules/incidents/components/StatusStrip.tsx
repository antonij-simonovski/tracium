import { SeverityTag } from '../../../common';
import hover from '../../../common/styles/hover.module.css';
import type { Incident } from '../interfaces';
import { bySeverity } from '../utils/incidents';

interface StatusStripProps {
  incidents: Incident[];
  total: number;
  onOpen: (id: string) => void;
  onViewAll: () => void;
}

export function StatusStrip({ incidents, total, onOpen, onViewAll }: StatusStripProps) {
  if (total === 0) {
    return <p style={{ fontSize: 14, color: 'var(--muted)', margin: '0 0 20px' }}>All workflows look healthy</p>;
  }
  const top = [...incidents].sort(bySeverity).slice(0, 3);
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 6, padding: '12px 16px', margin: '0 0 24px', border: '1px solid var(--border)', borderRadius: 10, background: 'var(--surface)' }}>
      {top.map((inc) => (
        <button
          key={inc.id}
          onClick={() => onOpen(inc.id)}
          className={hover.text}
          style={{ display: 'flex', alignItems: 'baseline', flexWrap: 'wrap', gap: '2px 10px', background: 'none', border: 'none', padding: '2px 0', textAlign: 'left', font: 'inherit', fontSize: 14, cursor: 'pointer' }}
        >
          <SeverityTag severity={inc.severity} compact />
          <span style={{ minWidth: 0, overflowWrap: 'anywhere' }}>{inc.title}</span>
        </button>
      ))}
      <button onClick={onViewAll} style={{ alignSelf: 'flex-start', background: 'none', border: 'none', padding: '4px 0 0', font: 'inherit', fontSize: 13, color: 'var(--accent)', cursor: 'pointer' }}>
        View all {total} →
      </button>
    </div>
  );
}
