import type { SpanId } from '../../../common/ids';
import type { AvailableTool, SetupIssue } from '../../../common/interfaces';
import type { TraceError } from './TraceError';

export interface SpanDetail {
  id: SpanId;
  name: string;
  // 'internal' is the view's local fallback for structural spans the collector
  // left unclassified; the others mirror the wire-level Span.kind enum.
  type: 'agent' | 'llm' | 'tool' | 'chain' | 'retriever' | 'embedding' | 'internal';
  start: number;
  duration: number;
  depth: number;
  cost: number;
  // Totals over the span and all its descendants, as computed by the API. When
  // absent the view shows the span's own cost and tokens.
  subtreeCost?: number;
  childCount?: number;
  tokens: number;
  subtreeTokens?: number;
  inputTokens?: number;
  outputTokens?: number;
  status: 'ok' | 'failed';
  error?: TraceError;
  input?: string;
  output?: string;
  availableTools?: AvailableTool[];
  setupIssues?: SetupIssue[];
  attributes: Record<string, string | number | boolean>;
}
