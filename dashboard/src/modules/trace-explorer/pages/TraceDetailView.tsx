import { useMemo, useState } from 'react';
import { useTrace } from '../hooks/useTrace';
import { toTraceView } from '../utils/toTraceView';
import { TraceView } from '../../trace-inspector';
import { Spinner, EmptyState } from '../../../common';
import { ReviewPanel } from '../components/ReviewPanel';
import { useReviewQueue } from '../hooks/useReview';
import { QUEUE_REASONS } from '../utils/queue';
import type { QueueReason } from '../../../common/interfaces';

export interface TraceDetailViewProps {
  traceId: string;
  setView: (v: string) => void;
  setSelected: (updater: (prev: Record<string, string>) => Record<string, string>) => void;
  /** Set when the run was opened from a workflow's review queue. */
  queue?: { workflow: string; reason: QueueReason };
}

export function TraceDetailView({ traceId, setView, setSelected, queue }: TraceDetailViewProps) {
  const { data, isLoading, error } = useTrace(traceId);
  const [saved, setSaved] = useState(false);
  const items = useReviewQueue(queue?.workflow ?? '', !!queue).data ?? [];
  const next = items.find(i => i.trace_id !== traceId);
  const view = useMemo(() => data && toTraceView(data), [data]);

  if (isLoading) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', padding: 48 }}>
        <Spinner size={32} />
      </div>
    );
  }

  if (error) {
    return (
      <div style={{ padding: 24, color: 'var(--error)' }}>
        Failed to load trace: {error instanceof Error ? error.message : 'Unknown error'}
      </div>
    );
  }

  if (!view) {
    return <EmptyState message="Trace not found" description="This trace is no longer available." />;
  }

  const openUser = (user: string) => {
    setSelected(s => ({ ...s, user }));
    setView('user');
  };
  const navigate = (v: string) => {
    if (v === 'workflows') setSelected(s => { const n = { ...s }; delete n.workflow; return n; });
    setView(v);
  };
  const openNext = () => {
    if (!next || !queue) return;
    setSaved(false);
    setSelected(s => ({ ...s, traceId: next.trace_id, queueTrace: next.trace_id, queue: queue.workflow, queueReason: next.reason }));
  };
  // Remount per trace so the selected span, collapsed set and tab don't carry over.
  return (
    <>
    {queue && (
      <div role="region" aria-label="Review queue" style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap', padding: '10px 14px', marginBottom: 16, border: '1px solid var(--border)', borderRadius: 8, fontSize: 13.5 }}>
        <span style={{ color: 'var(--muted)' }}>Review queue · {queue.workflow}</span>
        <span>{QUEUE_REASONS[queue.reason]}</span>
        {saved && <span style={{ color: 'var(--accent)' }}>Saved</span>}
        <span style={{ flex: 1 }} />
        <span style={{ color: 'var(--muted)' }}>{items.filter(i => i.trace_id !== traceId).length} left</span>
        <button type="button" onClick={openNext} disabled={!next}
          style={{ fontSize: 13.5, fontWeight: 500, padding: '6px 12px', borderRadius: 8, cursor: next ? 'pointer' : 'default', fontFamily: 'inherit', opacity: next ? 1 : 0.5, background: saved ? 'var(--foreground)' : 'transparent', color: saved ? 'var(--background)' : 'var(--foreground)', border: '1px solid var(--border)' }}>
          {next ? 'Next' : 'Queue empty'}
        </button>
      </div>
    )}
    <TraceView
      key={view.id}
      trace={view}
      setView={navigate}
      onOpenUser={openUser}
      review={<ReviewPanel traceId={view.id} output={view.output} reason={queue?.reason} onSaved={() => setSaved(true)} />}
      spanReview={span => <ReviewPanel key={span.id} traceId={view.id} spanId={span.id} output={span.output ?? null} />}
    />
    </>
  );
}
