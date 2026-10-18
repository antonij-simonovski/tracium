import type { TraceId, UserId } from '../../../common/ids';

export interface Trace {
  trace_id: TraceId;
  name: string;
  start_time_ms: number;
  end_time_ms: number;
  duration_ms: number;
  user_id: UserId;
  span_count: number;
  has_error: boolean;
  total_cost_usd: number;
  workflow_version?: string;
}
