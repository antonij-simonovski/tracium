import { relativeTime } from '../../../common';
import type { ReviewQueueItem } from '../../../common/interfaces';
import { QUEUE_REASONS, useReviewQueue } from '../../trace-explorer';
import { versionLabel } from '../utils';

const heading = { fontSize: 12, fontWeight: 500, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--muted)' } as const;

/** Unreviewed runs of the workflow worth reviewing next; each opens the run with its review panel. */
export function ReviewQueue({ workflow, onOpen }: { workflow: string; onOpen: (item: ReviewQueueItem) => void }) {
  const queue = useReviewQueue(workflow);
  const items = queue.data ?? [];
  return (
    <div style={{ marginBottom: 44 }}>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 12, paddingBottom: 14, marginBottom: 12, borderBottom: '1px solid var(--border)' }}>
        <span style={heading}>Review queue</span>
        <span style={{ fontSize: 13, color: 'var(--muted)' }}>Incident runs and flagged runs first; every fifth is a random sample</span>
      </div>
      {queue.isError ? <div style={{ fontSize: 13.5, color: 'var(--error)' }}>Couldn't load the review queue.</div>
        : queue.isLoading ? null
        : items.length === 0 ? <div style={{ fontSize: 13.5, color: 'var(--muted)' }}>Nothing to review: no unreviewed incident, flagged or recent runs.</div>
        : (
          <div role="list" aria-label="Review queue" style={{ border: '1px solid var(--border)', borderRadius: 8 }}>
            {items.map((it, i) => (
              <button key={it.trace_id} type="button" role="listitem" onClick={() => onOpen(it)}
                style={{ display: 'grid', gridTemplateColumns: 'minmax(0,1fr) 120px 80px 80px', gap: 12, alignItems: 'center', width: '100%', padding: '9px 12px', border: 'none', borderTop: i ? '1px solid var(--border)' : 'none', background: 'transparent', color: 'var(--foreground)', fontFamily: 'inherit', fontSize: 13, textAlign: 'left', cursor: 'pointer' }}>
                <span style={{ fontFamily: 'var(--font-mono)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{it.trace_id}</span>
                <span style={{ color: it.reason === 'sample' ? 'var(--muted)' : 'var(--warning)' }}>{QUEUE_REASONS[it.reason]}</span>
                <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--muted)' }}>{it.workflow_version ? versionLabel(it.workflow_version) : '—'}</span>
                <span style={{ color: 'var(--muted)', textAlign: 'right' }}>{relativeTime(it.start_time_ms)}</span>
              </button>
            ))}
          </div>
        )}
    </div>
  );
}
