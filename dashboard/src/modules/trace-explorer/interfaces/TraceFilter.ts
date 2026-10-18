import type { UserId } from '../../../common/ids';

export interface TraceFilter {
  user_id?: UserId;
  model?: string;
  /** Restrict to one workflow (the trace root name) — powers a workflow's recent runs. */
  workflow?: string;
  /** Restrict to the runs of one workflow version. */
  version?: string;
  /** Time window token (24h/7d/30d/90d/1y); bounds the listing server-side. */
  range?: string;
  has_error?: boolean;
  start_after?: string;
  start_before?: string;
  page?: number;
  page_size?: number;
}
