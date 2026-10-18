import { describe, it, expect } from 'vitest';
import { toTraceView } from './toTraceView';
import type { TraceDetail, Span } from '../interfaces';

// Minimal live span; only the fields under test matter, the rest are filler.
// Keys are checked against Span but values stay loose so tests can pass plain
// strings for branded ids (trace_id/span_id).
function span(partial: Partial<Record<keyof Span, unknown>>): Span {
  return {
    trace_id: 't1', span_id: 's', parent_span_id: '', name: 'span',
    start_time_ms: 1000, end_time_ms: 1000, duration_ms: 0,
    model: '', finish_reason: '', input_tokens: 0, output_tokens: 0,
    cost_usd: 0, subtree_cost_usd: 0, subtree_input_tokens: 0, subtree_output_tokens: 0, user_id: '', model_normalized: '', schema_version: 3,
    error_type: '', error_message: '',
    ...partial,
  } as unknown as Span;
}

function trace(spans: Span[]): TraceDetail {
  return {
    trace_id: 't1', name: 'workflow', start_time_ms: 1000, end_time_ms: 3000,
    duration_ms: 2000, user_id: '', span_count: spans.length,
    has_error: spans.some(s => !!s.error_type), total_cost_usd: 0, spans,
  } as unknown as TraceDetail;
}

describe('toTraceView', () => {
  it('maps per-span content and tools onto the view-model', () => {
    const tools = [{ name: 'search', description: 'web search', used: true }];
    const view = toTraceView(trace([
      span({ span_id: 'root', input: 'hi', output: 'bye', available_tools: tools, model: 'gpt-4o' }),
    ]));

    expect(view.spans[0].input).toBe('hi');
    expect(view.spans[0].output).toBe('bye');
    expect(view.spans[0].availableTools).toEqual(tools);
  });

  it('derives the workflow-level input/output from the root span', () => {
    const view = toTraceView(trace([
      span({ span_id: 'root', parent_span_id: '', input: 'workflow in', output: 'workflow out' }),
      span({ span_id: 'child', parent_span_id: 'root', input: 'child in', output: 'child out' }),
    ]));

    expect(view.input).toBe('workflow in');
    expect(view.output).toBe('workflow out');
  });

  it('falls back to child content when the root span is structural (no content)', () => {
    // OpenLLMetry workflow/task roots carry no LLM content; the prompt/completion
    // live on child gen_ai spans. The workflow tabs should show the earliest input
    // and latest output rather than nothing.
    const view = toTraceView(trace([
      span({ span_id: 'root', parent_span_id: '', start_time_ms: 1000 }),
      span({ span_id: 'first', parent_span_id: 'root', start_time_ms: 1100, input: 'first in', output: 'first out' }),
      span({ span_id: 'last', parent_span_id: 'root', start_time_ms: 1200, input: 'last in', output: 'last out' }),
    ]));

    expect(view.input).toBe('first in');
    expect(view.output).toBe('last out');
  });

  it('infers span type and depth from the model and parent chain', () => {
    const view = toTraceView(trace([
      span({ span_id: 'root', parent_span_id: '' }),
      span({ span_id: 'llm', parent_span_id: 'root', model: 'gpt-4o' }),
      span({ span_id: 'tool', parent_span_id: 'llm' }),
    ]));

    expect(view.spans.map(s => s.type)).toEqual(['agent', 'llm', 'tool']);
    expect(view.spans.map(s => s.depth)).toEqual([0, 1, 2]);
  });

  it('prefers the collector-classified kind over structural inference', () => {
    const view = toTraceView(trace([
      // Root carries an explicit kind: trust it instead of defaulting to 'agent'.
      span({ span_id: 'root', parent_span_id: '', kind: 'chain' }),
      // A span with tokens that would infer 'llm', but the collector says retriever.
      span({ span_id: 'r', parent_span_id: 'root', input_tokens: 50, kind: 'retriever' }),
      // No kind → falls back to inference (model present ⇒ llm).
      span({ span_id: 'm', parent_span_id: 'root', model: 'gpt-4o' }),
    ]));

    expect(view.spans.map(s => s.type)).toEqual(['chain', 'retriever', 'llm']);
  });

  it('classifies a non-LLM span as a tool under an LLM parent, internal otherwise', () => {
    const view = toTraceView(trace([
      span({ span_id: 'root', parent_span_id: '' }),
      span({ span_id: 'task', parent_span_id: 'root' }),               // structural — parent is the agent root
      span({ span_id: 'llm', parent_span_id: 'task', model: 'gpt-4o' }),
      span({ span_id: 'tool', parent_span_id: 'llm' }),                // tool — parent is an LLM call
    ]));

    expect(view.spans.map(s => s.type)).toEqual(['agent', 'internal', 'llm', 'tool']);
  });

  it('orders spans in tree pre-order even when a child shares the parent start time', () => {
    // The API returns the LLM child first (same start_time_ms tie). The view
    // must still render the root workflow span above its child.
    const view = toTraceView(trace([
      span({ span_id: 'llm', parent_span_id: 'root', model: 'gpt-4o', start_time_ms: 1000 }),
      span({ span_id: 'root', parent_span_id: '', start_time_ms: 1000 }),
    ]));

    expect(view.spans.map(s => s.id)).toEqual(['root', 'llm']);
    expect(view.spans.map(s => s.depth)).toEqual([0, 1]);
  });

  it('keeps parallel children grouped under their parent, ordered by start time', () => {
    const view = toTraceView(trace([
      span({ span_id: 'root', parent_span_id: '', start_time_ms: 1000 }),
      span({ span_id: 'b', parent_span_id: 'root', model: 'gpt-4o', start_time_ms: 1200 }),
      span({ span_id: 'a', parent_span_id: 'root', model: 'gpt-4o', start_time_ms: 1100 }),
    ]));

    expect(view.spans.map(s => s.id)).toEqual(['root', 'a', 'b']);
    expect(view.spans.map(s => s.depth)).toEqual([0, 1, 1]);
  });

  it('still renders every span when parent links form a cycle', () => {
    const view = toTraceView(trace([
      span({ span_id: 'x', parent_span_id: 'y' }),
      span({ span_id: 'y', parent_span_id: 'x' }),
    ]));

    expect(view.spans.map(s => s.id)).toEqual(['x', 'y']);
    expect(view.spans.map(s => s.depth)).toEqual([0, 1]);
  });

  it('keeps a cycle contiguous after the reachable tree', () => {
    const view = toTraceView(trace([
      span({ span_id: 'x', parent_span_id: 'y' }),
      span({ span_id: 'root', parent_span_id: '' }),
      span({ span_id: 'y', parent_span_id: 'x' }),
      span({ span_id: 'child', parent_span_id: 'root' }),
    ]));

    expect(view.spans.map(s => s.id)).toEqual(['root', 'child', 'x', 'y']);
    expect(view.spans.map(s => s.depth)).toEqual([0, 1, 0, 1]);
  });

  it('takes the trace error from the first failing span in tree order', () => {
    const view = toTraceView(trace([
      span({ span_id: 'late', parent_span_id: 'b', start_time_ms: 1100, error_type: 'Late' }),
      span({ span_id: 'root', parent_span_id: '', start_time_ms: 1000 }),
      span({ span_id: 'a', parent_span_id: 'root', start_time_ms: 1050 }),
      span({ span_id: 'early', parent_span_id: 'a', start_time_ms: 1200, error_type: 'Early', error_message: 'boom' }),
      span({ span_id: 'b', parent_span_id: 'root', start_time_ms: 1060 }),
    ]));

    expect(view.spans.find(s => s.status === 'failed')?.id).toBe('early');
    expect(view.error).toEqual({ type: 'Early', message: 'boom', code: '', stack: '' });
  });

  it('lists every distinct model the trace used', () => {
    const view = toTraceView(trace([
      span({ span_id: 'root', parent_span_id: '' }),
      span({ span_id: 'a', parent_span_id: 'root', model: 'claude-sonnet-4-5' }),
      span({ span_id: 'b', parent_span_id: 'root', model: 'gpt-4o' }),
      span({ span_id: 'c', parent_span_id: 'root', model: 'claude-sonnet-4-5' }),
    ]));

    expect(view.model).toBe('claude-sonnet-4-5, gpt-4o');
  });

  it('maps span setup issues onto the view-model', () => {
    const issues = [{ code: 'unpriced_model', severity: 'warning', message: 'No price for this model.' }];
    const view = toTraceView(trace([
      span({ span_id: 'root', setup_issues: issues }),
      span({ span_id: 'child', parent_span_id: 'root' }),
    ]));

    expect(view.spans[0].setupIssues).toEqual(issues);
    expect(view.spans[1].setupIssues).toBeUndefined();
  });

  it('leaves content fields undefined when the collector did not capture them', () => {
    const view = toTraceView(trace([span({ span_id: 'root' })]));
    expect(view.spans[0].input).toBeUndefined();
    expect(view.input).toBeUndefined();
    expect(view.output).toBeNull();
  });

  it('carries the trace client (user_id) and omits it when unset', () => {
    const withClient = toTraceView({ ...trace([span({ span_id: 'root' })]), user_id: 'acme' } as unknown as TraceDetail);
    expect(withClient.user).toBe('acme');
    expect(toTraceView(trace([span({ span_id: 'root' })])).user).toBeUndefined();
  });
});
