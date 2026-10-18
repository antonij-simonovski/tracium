import { useMemo } from 'react';
import { useTrace } from '../hooks/useTrace';
import { toTraceView } from '../utils/toTraceView';
import { TraceView } from '../../trace-inspector';
import { Spinner, EmptyState } from '../../../common';

export interface TraceDetailViewProps {
  traceId: string;
  setView: (v: string) => void;
  setSelected: (updater: (prev: Record<string, string>) => Record<string, string>) => void;
}

export function TraceDetailView({ traceId, setView, setSelected }: TraceDetailViewProps) {
  const { data, isLoading, error } = useTrace(traceId);
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
  // Remount per trace so the selected span, collapsed set and tab don't carry over.
  return <TraceView key={view.id} trace={view} setView={navigate} onOpenUser={openUser} />;
}
