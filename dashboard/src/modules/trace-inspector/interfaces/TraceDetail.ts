import type { TraceId } from '../../../common/ids';
import type { SessionId } from '../ids';
import type { SpanDetail } from './SpanDetail';
import type { TraceError } from './TraceError';

/**
 * Rich, presentation-ready shape consumed by the shared `TraceView`. The demo
 * fills every field; the live API supplies a flatter span model, so the fields
 * it can't provide are optional and `TraceView` omits their UI when absent.
 */
export interface TraceDetail {
  id: TraceId;
  workflow: string;
  status: 'completed' | 'failed';
  duration: number;
  totalCost: number;
  inputTokens: number;
  outputTokens: number;
  model: string;
  spans: SpanDetail[];
  error: TraceError | null;
  output: string | null;

  version?: string;
  startedAt?: string;
  endedAt?: string;
  cachedTokens?: number;
  provider?: string;
  user?: string;
  sessionId?: SessionId;
  environment?: string;
  region?: string;
  sdk?: string;
  input?: string;
  tags?: string[];
}
