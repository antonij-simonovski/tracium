import { relativeTime } from '../../../common';
import type { RunRow } from '../../../common';
import type { Trace } from '../interfaces';

// Status is the trace's error outcome, not workflow health.
export function toRunRow(t: Trace): RunRow {
  return {
    id: t.trace_id,
    status: t.has_error ? 'failed' : 'completed',
    time: relativeTime(t.start_time_ms),
    duration: t.duration_ms,
    cost: t.total_cost_usd,
    version: t.workflow_version,
  };
}
