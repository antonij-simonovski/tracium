import type { TraceId, SpanId, UserId } from '../../../common/ids';
import type { AvailableTool, SetupIssue } from '../../../common/interfaces';

export interface Span {
  trace_id: TraceId;
  span_id: SpanId;
  parent_span_id: SpanId;
  name: string;
  start_time_ms: number;
  end_time_ms: number;
  duration_ms: number;
  model: string;
  finish_reason: string;
  input_tokens: number;
  output_tokens: number;
  cost_usd: number;
  // Own cost plus the cost of every descendant span, computed by the API so
  // clients never have to walk the tree and re-sum it.
  subtree_cost_usd: number;
  // Token counts rolled up the same way as subtree_cost_usd: own tokens plus
  // every descendant's, so a parent reports the total consumed beneath it.
  subtree_input_tokens: number;
  subtree_output_tokens: number;
  user_id: UserId;
  model_normalized: string;
  schema_version: number;
  error_type: string;
  error_message: string;
  // Normalized span role classified by the collector from instrumentation
  // attributes. Absent when the collector couldn't classify the span; the view
  // then infers the role from the span's position in the trace tree.
  kind?: 'agent' | 'llm' | 'tool' | 'chain' | 'retriever' | 'embedding';

  // Content & tools (schema v3, all optional). input/output are present only
  // when the collector has content capture enabled.
  input?: string;
  output?: string;
  available_tools?: AvailableTool[];
  setup_issues?: SetupIssue[];
}
