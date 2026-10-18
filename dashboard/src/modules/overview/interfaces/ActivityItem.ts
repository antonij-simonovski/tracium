import type { TraceId } from '../../../common/ids';

export interface ActivityItem {
  id: TraceId;
  workflow: string;
  status: 'completed' | 'failed';
  time: string;
  latency: number;
  msg?: string;
}
