import { formatDateTime } from '../../../common';
import type { TraceDetail as LiveTrace, Span as LiveSpan } from '../interfaces';
import type { TraceDetail, SpanDetail } from '../../trace-inspector';

function isLlmSpan(span: LiveSpan): boolean {
  return !!span.model || span.input_tokens > 0 || span.output_tokens > 0;
}

// The collector's kind wins; without it the role is inferred from the tree: the
// root is the agent and a non-LLM span directly under an LLM call is its tool.
function spanType(span: LiveSpan, isRoot: boolean, parent: LiveSpan | undefined): SpanDetail['type'] {
  if (span.kind) return span.kind;
  if (isRoot) return 'agent';
  if (isLlmSpan(span)) return 'llm';
  return parent && isLlmSpan(parent) ? 'tool' : 'internal';
}

function spanAttributes(span: LiveSpan): SpanDetail['attributes'] {
  return Object.fromEntries(
    Object.entries({
      'llm.model': span.model,
      'llm.model_normalized': span.model_normalized,
      'finish_reason': span.finish_reason,
    }).filter(([, v]) => v != null && v !== ''),
  );
}

export function toTraceView(trace: LiveTrace): TraceDetail {
  const byId = new Map(trace.spans.map(s => [s.span_id, s]));
  const isRoot = (s: LiveSpan) => !s.parent_span_id || !byId.has(s.parent_span_id);

  // Emit spans in tree pre-order so every parent renders immediately above its
  // children. The API orders by start_time_ms alone, which ties when a parent
  // and child start in the same millisecond (e.g. an explicit span wrapping an
  // auto-instrumented LLM call) and can put the child above its parent.
  const childrenOf = new Map<string, LiveSpan[]>();
  for (const s of trace.spans) {
    const key = isRoot(s) ? '' : s.parent_span_id;
    (childrenOf.get(key) ?? childrenOf.set(key, []).get(key)!).push(s);
  }
  for (const group of childrenOf.values()) {
    group.sort((a, b) => a.start_time_ms - b.start_time_ms);
  }

  const ordered: { span: LiveSpan; depth: number }[] = [];
  const seen = new Set<string>();
  const visit = (span: LiveSpan, depth: number) => {
    if (seen.has(span.span_id)) return;
    seen.add(span.span_id);
    ordered.push({ span, depth });
    for (const child of childrenOf.get(span.span_id) ?? []) visit(child, depth + 1);
  };
  for (const r of childrenOf.get('') ?? []) visit(r, 0);
  // Spans caught in a parent-link cycle are unreachable from any root; start a tree at each.
  for (const s of trace.spans) visit(s, 0);

  const spans: SpanDetail[] = ordered.map(({ span: s, depth }) => ({
    id: s.span_id,
    name: s.name,
    type: spanType(s, isRoot(s), byId.get(s.parent_span_id)),
    start: s.start_time_ms - trace.start_time_ms,
    duration: s.duration_ms,
    depth,
    cost: s.cost_usd,
    subtreeCost: s.subtree_cost_usd,
    childCount: (childrenOf.get(s.span_id) ?? []).length,
    tokens: s.input_tokens + s.output_tokens,
    subtreeTokens: s.subtree_input_tokens + s.subtree_output_tokens,
    inputTokens: s.input_tokens,
    outputTokens: s.output_tokens,
    status: s.error_type || s.error_message ? 'failed' : 'ok',
    error: s.error_type || s.error_message
      ? { type: s.error_type || 'error', message: s.error_message || '', code: '', stack: '' }
      : undefined,
    attributes: spanAttributes(s),
    input: s.input,
    output: s.output,
    availableTools: s.available_tools,
    setupIssues: s.setup_issues,
  }));

  // The workflow-level Input/Output tabs mirror the root span's content. But many
  // instrumentations (e.g. OpenLLMetry workflow/task decorators) make the root a
  // structural span with no LLM content, while the actual prompt/completion live
  // on child gen_ai spans. Fall back to the earliest input and latest output that
  // were actually recorded so the tabs aren't empty for those traces.
  const root = trace.spans.find(isRoot);
  const byStart = [...trace.spans].sort((a, b) => a.start_time_ms - b.start_time_ms);
  const traceInput = root?.input || byStart.find(s => s.input)?.input;
  const traceOutput = root?.output || [...byStart].reverse().find(s => s.output)?.output;

  return {
    id: trace.trace_id,
    workflow: trace.name || 'Untitled trace',
    status: trace.has_error ? 'failed' : 'completed',
    startedAt: formatDateTime(trace.start_time_ms),
    endedAt: formatDateTime(trace.end_time_ms),
    duration: trace.duration_ms,
    totalCost: trace.total_cost_usd,
    inputTokens: trace.spans.reduce((sum, s) => sum + s.input_tokens, 0),
    outputTokens: trace.spans.reduce((sum, s) => sum + s.output_tokens, 0),
    model: [...new Set(trace.spans.map(s => s.model).filter(Boolean))].join(', '),
    user: trace.user_id || undefined,
    input: traceInput,
    output: traceOutput ?? null,
    spans,
    error: spans.find(s => s.error)?.error ?? null,
  };
}
