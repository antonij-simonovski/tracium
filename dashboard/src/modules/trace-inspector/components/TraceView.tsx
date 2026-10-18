import React, { memo, useCallback, useMemo, useState } from 'react';
import {
  IconUser,
  IconX,
  MetaRow,
  SetupIssueNote,
  StatTile,
  StatusPill,
  fmtCost,
  fmtNum,
  fmtMs,
  plural,
  useMaxWidth,
  BREAKPOINTS,
  SEVERITY_META,
  useCopy,
} from '../../../common';
import type { AvailableTool } from '../../../common/interfaces';
import type { SpanDetail, TraceDetail } from '../interfaces';
import type { TabId } from '../ids';
import { prettifyMaybeJson } from '../utils';

export interface TraceViewProps {
  trace: TraceDetail;
  setView: (v: string) => void;
  onOpenUser?: (user: string) => void;
}

function CodeBlock({ children, maxHeight = 220 }: { children: string; maxHeight?: number }) {
  return (
    <pre style={{
      margin: 0, padding: '14px 16px',
      background: 'color-mix(in srgb, var(--surface-alt) 60%, transparent)',
      border: '1px solid color-mix(in srgb, var(--border) 60%, transparent)',
      borderRadius: 8,
      fontFamily: 'var(--font-mono)', fontSize: 13, lineHeight: 1.55,
      color: 'var(--foreground)',
      whiteSpace: 'pre-wrap', wordBreak: 'break-word',
      maxHeight, overflowY: 'auto',
    }}>{children}</pre>
  );
}

function JsonBlock({ text, maxHeight }: { text: string; maxHeight?: number }) {
  const pretty = useMemo(() => prettifyMaybeJson(text), [text]);
  return <CodeBlock maxHeight={maxHeight}>{pretty}</CodeBlock>;
}

function EmptyBlock({ children }: { children: string }) {
  return (
    <div style={{ padding: '28px 18px', border: '1px dashed var(--border)', borderRadius: 10, color: 'var(--muted)', fontSize: 14, textAlign: 'center' }}>
      {children}
    </div>
  );
}

function SectionLabel({ children, tone, style }: { children: string; tone?: 'error'; style?: React.CSSProperties }) {
  return (
    <div style={{
      fontSize: 12, fontWeight: 500, textTransform: 'uppercase', letterSpacing: '0.08em',
      color: tone === 'error' ? 'var(--error)' : 'var(--muted)', marginBottom: 8, ...style,
    }}>{children}</div>
  );
}

const SPAN_GRID = 'minmax(220px, 1.5fr) 70px 64px 72px 2fr';
const TICKS = 5;
const TRACK_BACKGROUND =
  `linear-gradient(to left, color-mix(in srgb, var(--foreground) 4%, transparent) 1px, transparent 1px) 0 0 / ${100 / TICKS}% 100%, ` +
  'color-mix(in srgb, var(--border) 55%, transparent)';

const TYPE_COLORS: Record<SpanDetail['type'], string> = {
  agent: 'var(--foreground)',
  llm: 'var(--accent)',
  tool: '#7aa5ff',
  chain: '#a78bfa',
  retriever: '#5ec5a8',
  embedding: '#e0a458',
  internal: 'var(--muted)',
};

function SpanTypeTag({ type }: { type: SpanDetail['type'] }) {
  const c = TYPE_COLORS[type];
  return (
    <span style={{
      fontSize: 11.5, fontWeight: 500, textTransform: 'uppercase', letterSpacing: '0.08em',
      padding: '2px 7px', borderRadius: 4, color: c,
      background: `color-mix(in srgb, ${c} 12%, transparent)`,
      border: `1px solid color-mix(in srgb, ${c} 22%, transparent)`,
    }}>{type}</span>
  );
}

function isParentSpan(span: SpanDetail): boolean {
  return (span.childCount ?? 0) > 0;
}

// Parents show their subtree totals; spans without a subtree figure show their own.
function displayCost(span: SpanDetail): number {
  return span.subtreeCost ?? span.cost;
}

function displayTokens(span: SpanDetail): number {
  return span.subtreeTokens ?? span.tokens;
}

// Structural agent/internal spans stay muted so the billable roles stand out.
function spanColor(span: SpanDetail): string {
  if (span.status === 'failed') return 'var(--error)';
  if (span.type === 'agent' || span.type === 'internal') return 'var(--muted)';
  return TYPE_COLORS[span.type];
}

function AvailableToolsList({ tools }: { tools: AvailableTool[] }) {
  const [showAll, setShowAll] = useState(false);
  const usedCount = tools.filter(t => t.used).length;
  const visible = showAll ? tools : tools.filter(t => t.used);

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, marginBottom: 10 }}>
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 8 }}>
          <span style={{ fontSize: 12, fontWeight: 500, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--muted)' }}>
            Available tools
          </span>
          <span style={{ fontSize: 12, color: 'var(--muted)', fontVariantNumeric: 'tabular-nums' }}>
            {usedCount} of {tools.length} used
          </span>
        </div>
        <div style={{ display: 'inline-flex', padding: 2, background: 'var(--surface-alt)', border: '1px solid var(--border)', borderRadius: 6 }}>
          {[{ id: false, label: 'Used' }, { id: true, label: 'All' }].map(opt => (
            <button key={String(opt.id)} onClick={() => setShowAll(opt.id)} style={{
              padding: '3px 10px', fontSize: 12.5, fontWeight: 500, border: 'none', borderRadius: 4,
              background: showAll === opt.id ? 'var(--surface)' : 'transparent',
              color: showAll === opt.id ? 'var(--foreground)' : 'var(--muted)',
              boxShadow: showAll === opt.id ? '0 0 0 1px var(--border)' : 'none',
              cursor: 'pointer', fontFamily: 'inherit',
            }}>{opt.label}</button>
          ))}
        </div>
      </div>
      <div style={{ border: '1px solid var(--border)', borderRadius: 8, background: 'var(--surface)', overflow: 'hidden' }}>
        {visible.length === 0 && (
          <div style={{ padding: '16px 14px', fontSize: 13.5, color: 'var(--muted)', textAlign: 'center' }}>
            No tools were called in this span.
          </div>
        )}
        {visible.map((tool, i) => (
          <div key={tool.name} style={{
            display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '2px 12px',
            padding: '10px 12px',
            borderTop: i === 0 ? 'none' : '1px solid color-mix(in srgb, var(--border) 50%, transparent)',
            opacity: tool.used ? 1 : 0.65,
          }}>
            <span style={{
              gridRow: '1 / span 2', alignSelf: 'center',
              width: 6, height: 6, borderRadius: '50%',
              background: tool.used ? 'var(--accent)' : 'var(--muted)',
              display: 'inline-block',
            }} />
            <span style={{ fontSize: 13.5, fontWeight: 500, fontFamily: 'var(--font-mono)', color: 'var(--foreground)' }}>
              {tool.name}
            </span>
            <span style={{ fontSize: 13, color: 'var(--muted)', lineHeight: 1.5 }}>{tool.description}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

function SpanInspector({ span }: { span: SpanDetail | undefined }) {
  if (!span) return null;
  const isParent = isParentSpan(span);
  const cost = displayCost(span);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
          <SpanTypeTag type={span.type} />
          <StatusPill status={span.status === 'ok' ? 'completed' : 'failed'} />
        </div>
        <div style={{ fontSize: 17, fontWeight: 600, color: 'var(--foreground)', letterSpacing: '-0.01em', wordBreak: 'break-word' }}>
          {span.name}
        </div>
        <div style={{ fontSize: 13, color: 'var(--muted)', fontFamily: 'var(--font-mono)' }}>{span.id}</div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', borderTop: '1px solid var(--border)', borderBottom: '1px solid var(--border)' }}>
        <div style={{ padding: '12px 14px', borderRight: '1px solid var(--border)' }}>
          <div style={{ fontSize: 12, color: 'var(--muted)', marginBottom: 4 }}>Duration</div>
          <div style={{ fontSize: 18, fontWeight: 500, fontVariantNumeric: 'tabular-nums', letterSpacing: '-0.015em' }}>{fmtMs(span.duration)}</div>
          <div style={{ fontSize: 12, color: 'var(--muted)', marginTop: 3 }}>starts at +{fmtMs(span.start)}</div>
        </div>
        <div style={{ padding: '12px 14px' }}>
          <div style={{ fontSize: 12, color: 'var(--muted)', marginBottom: 4 }}>{isParent ? 'Cost (subtree)' : 'Cost'}</div>
          <div style={{ fontSize: 18, fontWeight: 500, fontVariantNumeric: 'tabular-nums', letterSpacing: '-0.015em' }}>
            {cost > 0 ? fmtCost(cost) : '—'}
          </div>
          {isParent ? (
            <div style={{ fontSize: 12, color: 'var(--muted)', marginTop: 3, fontVariantNumeric: 'tabular-nums' }}>
              {span.cost > 0 ? `${fmtCost(span.cost)} self · ` : ''}{plural(span.childCount ?? 0, 'child span')}
            </div>
          ) : span.tokens > 0 && span.inputTokens != null && (
            <div style={{ fontSize: 12, color: 'var(--muted)', marginTop: 3, fontVariantNumeric: 'tabular-nums' }}>
              {fmtNum(span.inputTokens)} in{span.outputTokens ? ` · ${fmtNum(span.outputTokens)} out` : ''}
            </div>
          )}
        </div>
      </div>

      {span.setupIssues && span.setupIssues.length > 0 && (
        <div>
          <SectionLabel>Setup issues</SectionLabel>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {[...span.setupIssues].sort((a, b) => SEVERITY_META[b.severity].rank - SEVERITY_META[a.severity].rank).map((issue) => <SetupIssueNote key={issue.code} issue={issue} />)}
          </div>
        </div>
      )}

      {span.error && (
        <div>
          <SectionLabel tone="error">{span.error.code ? `Error · HTTP ${span.error.code}` : 'Error'}</SectionLabel>
          <div style={{ fontSize: 13.5, color: 'var(--foreground)', marginBottom: 10, lineHeight: 1.5 }}>
            <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--error)' }}>{span.error.type}</span>
            {span.error.message && <span style={{ color: 'var(--muted)' }}> · </span>}
            {span.error.message}
          </div>
          {span.error.stack && <CodeBlock maxHeight={140}>{span.error.stack}</CodeBlock>}
        </div>
      )}

      {span.input && (
        <div>
          <SectionLabel>Input</SectionLabel>
          <JsonBlock text={span.input} />
        </div>
      )}

      {span.output && (
        <div>
          <SectionLabel>Output</SectionLabel>
          <JsonBlock text={span.output} />
        </div>
      )}

      {span.availableTools && span.availableTools.length > 0 && (
        <AvailableToolsList tools={span.availableTools} />
      )}

      {Object.keys(span.attributes).length > 0 && (
        <div>
          <SectionLabel style={{ marginBottom: 4 }}>Attributes</SectionLabel>
          <div>
            {Object.entries(span.attributes).map(([k, v]) => (
              <MetaRow key={k} label={k} value={v} mono />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function HeaderButton({ children, primary, onClick }: { children: React.ReactNode; primary?: boolean; onClick?: () => void }) {
  return (
    <button onClick={onClick} style={{
      fontSize: 13.5, fontWeight: 500, padding: '7px 12px', borderRadius: 8,
      display: 'flex', alignItems: 'center', gap: 6, cursor: 'pointer',
      fontFamily: 'inherit', whiteSpace: 'nowrap',
      background: primary ? 'var(--accent)' : 'transparent',
      border: `1px solid ${primary ? 'var(--accent)' : 'var(--border)'}`,
      color: primary ? 'var(--accent-contrast)' : 'var(--foreground)',
    }}>{children}</button>
  );
}

function ClientChip({ id, onOpen }: { id: string; onOpen?: (id: string) => void }) {
  const style: React.CSSProperties = {
    display: 'inline-flex', alignItems: 'center', gap: 6, maxWidth: '100%',
    padding: '3px 10px 3px 8px', borderRadius: 999,
    border: '1px solid var(--border)', background: 'var(--surface)',
    fontSize: 13, fontFamily: 'inherit', color: 'var(--muted)',
  };
  const content = (
    <>
      <IconUser size={13} />
      <span>Client</span>
      <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--foreground)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{id}</span>
    </>
  );
  return onOpen
    ? <button type="button" title="Open client" onClick={() => onOpen(id)} style={{ ...style, cursor: 'pointer' }}>{content}</button>
    : <span style={style}>{content}</span>;
}

export function TraceView({ trace: t, setView, onOpenUser }: TraceViewProps) {
  // A zero-duration trace would divide by zero in the timeline math.
  const totalDuration = t.duration || 1;
  const firstFailed = t.spans.find(s => s.status === 'failed');
  const [activeSpanId, setActiveSpanId] = useState<string>(firstFailed?.id ?? t.spans[0]?.id ?? '');
  const [tab, setTab] = useState<TabId>('timeline');
  const [shared, copyLink] = useCopy();
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set());

  const toggleCollapse = useCallback((id: string) =>
    setCollapsed(prev => {
      const next = new Set(prev);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    }), []);

  // Spans are in tree pre-order, so a span's ancestors are the nearest preceding
  // spans at each shallower depth; expanding them reveals the target row.
  const revealSpan = (id: string) => {
    const idx = t.spans.findIndex(s => s.id === id);
    if (idx >= 0) {
      const ancestors: string[] = [];
      let depth = t.spans[idx].depth;
      for (let i = idx - 1; i >= 0 && depth > 0; i--) {
        if (t.spans[i].depth < depth) {
          ancestors.push(t.spans[i].id);
          depth = t.spans[i].depth;
        }
      }
      if (ancestors.length > 0) {
        setCollapsed(prev => {
          const next = new Set(prev);
          ancestors.forEach(a => next.delete(a));
          return next;
        });
      }
    }
    setActiveSpanId(id);
    setTab('timeline');
  };

  // A span's descendants are the contiguous deeper spans that follow it.
  const visibleSpans = useMemo(() => {
    const visible: SpanDetail[] = [];
    let hideBelow: number | null = null;
    for (const span of t.spans) {
      if (hideBelow !== null) {
        if (span.depth > hideBelow) continue;
        hideBelow = null;
      }
      visible.push(span);
      if (collapsed.has(span.id) && isParentSpan(span)) hideBelow = span.depth;
    }
    return visible;
  }, [t.spans, collapsed]);

  const activeSpan = t.spans.find(s => s.id === activeSpanId);
  const failedCount = t.spans.filter(s => s.status === 'failed').length;

  const tickValues = Array.from({ length: TICKS + 1 }, (_, i) => (totalDuration / TICKS) * i);

  const subtitle = [
    t.version && { text: t.version },
    t.model && { text: t.model, mono: true },
    t.environment && { text: t.environment },
  ].filter(Boolean) as { text: string; mono?: boolean }[];

  const tabs: { id: TabId; label: string }[] = [
    { id: 'timeline', label: 'Timeline' },
    { id: 'input', label: 'Input' },
    { id: 'output', label: 'Output' },
    { id: 'metadata', label: 'Metadata' },
    { id: 'raw', label: 'Raw JSON' },
  ];

  const stackInspector = useMaxWidth(BREAKPOINTS.tablet);

  return (
    <div style={{ padding: 'clamp(20px, 4vw, 32px) clamp(16px, 4vw, 40px) 64px', maxWidth: 1480, margin: '0 auto' }}>
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 24, flexWrap: 'wrap', marginBottom: 24 }}>
        <div style={{ minWidth: 0 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10, fontSize: 13, flexWrap: 'wrap' }}>
            <button type="button" onClick={() => setView('workflows')} style={{ all: 'unset', color: 'var(--muted)', cursor: 'pointer' }}>Workflows</button>
            <span style={{ color: 'var(--muted)', opacity: 0.4 }}>/</span>
            <span style={{ color: 'var(--muted)' }}>{t.workflow}</span>
            <span style={{ color: 'var(--muted)', opacity: 0.4 }}>/</span>
            <span style={{ color: 'var(--foreground)', fontFamily: 'var(--font-mono)' }}>{t.id}</span>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 10, flexWrap: 'wrap' }}>
            <StatusPill status={t.status} />
            {subtitle.map((p, i) => (
              <React.Fragment key={i}>
                {i > 0 && <span style={{ color: 'var(--muted)', opacity: 0.4 }}>·</span>}
                <span style={{ fontSize: 13, color: 'var(--muted)', fontFamily: p.mono ? 'var(--font-mono)' : 'inherit' }}>{p.text}</span>
              </React.Fragment>
            ))}
          </div>
          <h1 style={{ fontSize: 26, fontWeight: 600, letterSpacing: '-0.02em', margin: '0 0 6px', color: 'var(--foreground)' }}>{t.workflow}</h1>
          {(t.startedAt || t.user) && (
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px 14px', flexWrap: 'wrap', fontSize: 13.5, color: 'var(--muted)' }}>
              {t.startedAt && (
                <span>{t.startedAt}{t.endedAt && <> <span style={{ opacity: 0.4 }}>→</span> {t.endedAt}</>}</span>
              )}
              {t.user && <ClientChip id={t.user} onOpen={onOpenUser} />}
            </div>
          )}
        </div>
        <div style={{ display: 'flex', gap: 8, flexShrink: 0 }}>
          <HeaderButton onClick={() => copyLink(window.location.href)}>{shared === 'copied' ? 'Link copied' : shared === 'failed' ? 'Copy failed' : 'Share'}</HeaderButton>
        </div>
      </div>

      {t.error && (
        <div style={{
          padding: '14px 0',
          borderTop: '1px solid color-mix(in srgb, var(--error) 28%, transparent)',
          borderBottom: '1px solid color-mix(in srgb, var(--error) 28%, transparent)',
          marginBottom: 28, display: 'flex', alignItems: 'flex-start', gap: 12,
        }}>
          <span style={{
            display: 'grid', placeItems: 'center',
            width: 28, height: 28, borderRadius: 8,
            background: 'color-mix(in srgb, var(--error) 18%, transparent)',
            color: 'var(--error)', flexShrink: 0,
          }}>
            <IconX size={14} />
          </span>
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 14.5, fontWeight: 500, color: 'var(--foreground)', marginBottom: 4 }}>
              {t.error.code && <>HTTP {t.error.code} · </>}<span style={{ fontFamily: 'var(--font-mono)' }}>{t.error.type}</span>
            </div>
            <div style={{ fontSize: 13.5, color: 'var(--muted)', lineHeight: 1.5 }}>{t.error.message}</div>
          </div>
          {firstFailed && (
            <button
              onClick={() => revealSpan(firstFailed.id)}
              style={{
                fontSize: 13, fontWeight: 500, color: 'var(--error)', padding: '5px 10px',
                border: '1px solid color-mix(in srgb, var(--error) 28%, transparent)', borderRadius: 7,
                background: 'transparent', whiteSpace: 'nowrap', cursor: 'pointer', fontFamily: 'inherit',
              }}
            >Jump to failing span →</button>
          )}
        </div>
      )}

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(130px, 1fr))', borderTop: '1px solid var(--border)', borderBottom: '1px solid var(--border)', margin: '4px 0 36px' }}>
        <StatTile isFirst label="Duration" value={fmtMs(t.duration)} />
        <StatTile label="Total cost" value={fmtCost(t.totalCost)} />
        <StatTile label="Input tokens" value={fmtNum(t.inputTokens)} sub={t.cachedTokens != null ? `${fmtNum(t.cachedTokens)} cached` : undefined} />
        <StatTile label="Output tokens" value={fmtNum(t.outputTokens)} />
        <StatTile label="Spans" value={t.spans.length} sub={`${failedCount} failed`} tone={failedCount > 0 ? 'bad' : undefined} />
        <StatTile label="Model" value={t.model || '—'} sub={t.provider} />
      </div>

      <div role="tablist" style={{ display: 'flex', alignItems: 'center', gap: 4, borderBottom: '1px solid var(--border)', marginBottom: 24 }}>
        {tabs.map(tb => (
          <button key={tb.id} role="tab" aria-selected={tab === tb.id} onClick={() => setTab(tb.id)} style={{
            padding: '10px 14px', fontSize: 14, fontWeight: 500,
            color: tab === tb.id ? 'var(--foreground)' : 'var(--muted)',
            background: 'transparent', border: 'none',
            borderBottom: '2px solid ' + (tab === tb.id ? 'var(--accent)' : 'transparent'),
            marginBottom: -1, cursor: 'pointer', fontFamily: 'inherit',
          }}>{tb.label}</button>
        ))}
      </div>

      {tab === 'timeline' && (
        <div style={{ display: 'grid', gridTemplateColumns: stackInspector ? '1fr' : '1fr 420px', gap: 32, alignItems: 'start' }}>
          <div style={{ minWidth: 0, overflowX: 'auto' }}>
            <div style={{
              display: 'grid',
              gridTemplateColumns: SPAN_GRID,
              minWidth: 560,
              gap: 14, padding: '10px 4px',
              fontSize: 12, color: 'var(--muted)', fontWeight: 500,
              textTransform: 'uppercase', letterSpacing: '0.06em',
              borderBottom: '1px solid var(--border)',
            }}>
              <span>Span</span>
              <span style={{ textAlign: 'right' }}>Duration</span>
              <span style={{ textAlign: 'right' }}>Tokens</span>
              <span style={{ textAlign: 'right' }}>Cost</span>
              <div style={{ position: 'relative', height: 16 }}>
                {tickValues.map((v, i) => (
                  <span key={i} style={{
                    position: 'absolute', left: (v / totalDuration) * 100 + '%',
                    transform: 'translateX(-50%)', fontSize: 11.5, color: 'var(--muted)',
                    opacity: 0.7, whiteSpace: 'nowrap',
                  }}>{fmtMs(v)}</span>
                ))}
              </div>
            </div>

            {visibleSpans.map((span, i) => (
              <SpanRow
                key={span.id}
                span={span}
                isLast={i === visibleSpans.length - 1}
                isActive={activeSpanId === span.id}
                isCollapsed={collapsed.has(span.id)}
                totalDuration={totalDuration}
                onSelect={setActiveSpanId}
                onToggleCollapse={toggleCollapse}
              />
            ))}

            <div style={{
              display: 'flex', alignItems: 'center', gap: 18,
              padding: '14px 4px 0', borderTop: '1px solid var(--border)',
              fontSize: 13, color: 'var(--muted)', marginTop: 6,
            }}>
              <LegendDot color={TYPE_COLORS.llm} label="LLM" />
              <LegendDot color={TYPE_COLORS.tool} label="Tool" />
              <LegendDot color={TYPE_COLORS.chain} label="Chain" />
              <LegendDot color={TYPE_COLORS.retriever} label="Retriever" />
              <LegendDot color={TYPE_COLORS.embedding} label="Embedding" />
              <LegendDot color={TYPE_COLORS.internal} label="Internal" />
              <LegendDot color="var(--error)" label="Failed" />
              <span style={{ marginLeft: 'auto', fontVariantNumeric: 'tabular-nums' }}>
                {t.spans.length} spans · {fmtMs(totalDuration)} total
              </span>
            </div>
          </div>

          <div style={{
            position: 'sticky', top: 90,
            paddingLeft: 24, borderLeft: '1px solid var(--border)',
            maxHeight: 'calc(100vh - 120px)', overflowY: 'auto',
          }}>
            <SpanInspector span={activeSpan} />
          </div>
        </div>
      )}

      {tab === 'input' && (
        <div style={{ maxWidth: 820 }}>
          <SectionLabel style={{ marginBottom: 10 }}>Workflow input</SectionLabel>
          {t.input ? <JsonBlock text={t.input} maxHeight={500} /> : <EmptyBlock>No input recorded for this trace.</EmptyBlock>}
        </div>
      )}

      {tab === 'output' && (
        <div style={{ maxWidth: 820 }}>
          <SectionLabel style={{ marginBottom: 10 }}>Workflow output</SectionLabel>
          {t.output
            ? <JsonBlock text={t.output} maxHeight={500} />
            : <EmptyBlock>{t.status === 'failed' ? 'No output. The trace failed before completion.' : 'No output recorded for this trace.'}</EmptyBlock>}
        </div>
      )}

      {tab === 'metadata' && (
        <div style={{ display: 'grid', gridTemplateColumns: stackInspector ? '1fr' : '1fr 1fr', gap: 40, maxWidth: 1040 }}>
          <div>
            <SectionLabel>Identity</SectionLabel>
            <MetaRow label="trace.id" value={t.id} mono />
            <MetaRow label="workflow" value={t.workflow} />
            <MetaRow label="workflow.version" value={t.version} />
            <MetaRow label="session.id" value={t.sessionId} mono />
            <MetaRow label="user.id" value={t.user} mono onClick={t.user && onOpenUser ? () => onOpenUser(t.user!) : undefined} />
            <SectionLabel style={{ margin: '24px 0 8px' }}>Timing</SectionLabel>
            <MetaRow label="started_at" value={t.startedAt} />
            <MetaRow label="ended_at" value={t.endedAt} />
            <MetaRow label="duration" value={fmtMs(t.duration)} />
          </div>
          <div>
            <SectionLabel>Runtime</SectionLabel>
            <MetaRow label="llm.model" value={t.model} mono />
            <MetaRow label="llm.provider" value={t.provider} />
            <MetaRow label="environment" value={t.environment} />
            <MetaRow label="region" value={t.region} />
            <MetaRow label="sdk" value={t.sdk} mono />
            {t.tags && t.tags.length > 0 && (
              <>
                <SectionLabel style={{ margin: '24px 0 8px' }}>Tags</SectionLabel>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, padding: '4px 0' }}>
                  {t.tags.map(tag => (
                    <span key={tag} style={{
                      fontSize: 12.5, padding: '3px 10px', borderRadius: 999,
                      color: 'var(--muted)', border: '1px solid var(--border)', background: 'transparent',
                    }}>{tag}</span>
                  ))}
                </div>
              </>
            )}
          </div>
        </div>
      )}

      {tab === 'raw' && (
        <div style={{ maxWidth: 1040 }}>
          <SectionLabel style={{ marginBottom: 10 }}>Raw trace document</SectionLabel>
          <CodeBlock maxHeight={600}>{JSON.stringify(t, null, 2)}</CodeBlock>
        </div>
      )}
    </div>
  );
}

interface SpanRowProps {
  span: SpanDetail;
  isLast: boolean;
  isActive: boolean;
  isCollapsed: boolean;
  totalDuration: number;
  onSelect: (id: string) => void;
  onToggleCollapse: (id: string) => void;
}

const SpanRow = memo(function SpanRow({ span, isLast, isActive, isCollapsed, totalDuration, onSelect, onToggleCollapse }: SpanRowProps) {
  const worstIssue = span.setupIssues?.length ? span.setupIssues.reduce((a, b) => (SEVERITY_META[b.severity].rank > SEVERITY_META[a.severity].rank ? b : a)) : undefined;
  const color = spanColor(span);
  const leftPct = (span.start / totalDuration) * 100;
  const widthPct = Math.max(0.4, (span.duration / totalDuration) * 100);
  const isParent = isParentSpan(span);
  const rowCost = displayCost(span);
  const rowTokens = displayTokens(span);
  const subtreeTitle = isParent ? `Subtree total over ${plural(span.childCount ?? 0, 'child span')}` : undefined;
  return (
    <div style={{ position: 'relative' }}>
      <button
        onClick={() => onSelect(span.id)}
        aria-pressed={isActive}
        style={{
          display: 'grid',
          gridTemplateColumns: SPAN_GRID,
          minWidth: 560,
          gap: 14, width: '100%', padding: '11px 4px',
          textAlign: 'left', border: 'none',
          borderBottom: isLast ? 'none' : '1px solid color-mix(in srgb, var(--border) 50%, transparent)',
          alignItems: 'center',
          background: isActive
            ? 'var(--accent-soft)'
            : span.status === 'failed'
            ? 'color-mix(in srgb, var(--error) 5%, transparent)'
            : 'transparent',
          borderLeft: '2px solid ' + (isActive ? 'var(--accent)' : 'transparent'),
          cursor: 'pointer', fontFamily: 'inherit',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, paddingLeft: span.depth * 16, minWidth: 0 }}>
          <span style={{ width: 16, flexShrink: 0, textAlign: 'center', color: 'var(--muted)', fontSize: 13, opacity: 0.5 }}>
            {!isParent && span.depth > 0 ? '└' : ''}
          </span>
          <span style={{ display: 'inline-block', width: 7, height: 7, background: color, borderRadius: 2, flexShrink: 0 }} />
          <span style={{ fontSize: 14, fontWeight: 500, color: 'var(--foreground)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{span.name}</span>
          <SpanTypeTag type={span.type} />
          {worstIssue && (
            <span
              title={plural(span.setupIssues!.length, 'setup issue')}
              style={{ width: 7, height: 7, borderRadius: '50%', background: SEVERITY_META[worstIssue.severity].color, flexShrink: 0 }}
            />
          )}
        </div>
        <span style={{ fontSize: 13.5, textAlign: 'right', color: 'var(--foreground)', fontVariantNumeric: 'tabular-nums' }}>{fmtMs(span.duration)}</span>
        <span
          title={subtreeTitle}
          style={{ fontSize: 13.5, textAlign: 'right', color: rowTokens > 0 ? 'var(--foreground)' : 'var(--muted)', fontVariantNumeric: 'tabular-nums' }}
        >
          {rowTokens > 0 ? fmtNum(rowTokens) : '—'}
        </span>
        <span
          title={subtreeTitle}
          style={{ fontSize: 13.5, textAlign: 'right', color: rowCost > 0 ? 'var(--foreground)' : 'var(--muted)', fontVariantNumeric: 'tabular-nums' }}
        >
          {rowCost > 0 ? fmtCost(rowCost) : '—'}
        </span>
        <div style={{ position: 'relative', height: 22, background: TRACK_BACKGROUND, borderRadius: 4, overflow: 'hidden' }}>
          <div style={{
            position: 'absolute', top: 0, bottom: 0,
            left: leftPct + '%', width: widthPct + '%',
            background: color, opacity: 0.9, borderRadius: 3,
            display: 'flex', alignItems: 'center', paddingLeft: 6,
          }}>
            {widthPct > 14 && (
              <span style={{ fontSize: 11.5, color: 'var(--accent-contrast)', fontWeight: 500, whiteSpace: 'nowrap' }}>
                {fmtMs(span.duration)}
              </span>
            )}
          </div>
        </div>
      </button>
      {isParent && (
        <button
          type="button"
          aria-expanded={!isCollapsed}
          aria-label={`${isCollapsed ? 'Expand' : 'Collapse'} ${span.name}`}
          title={isCollapsed ? `Show ${plural(span.childCount ?? 0, 'hidden child span')}` : 'Hide child spans'}
          onClick={() => onToggleCollapse(span.id)}
          style={{
            position: 'absolute', top: '50%', left: 6 + span.depth * 16, transform: 'translateY(-50%)',
            display: 'grid', placeItems: 'center', width: 16, height: 16, padding: 0,
            border: 'none', borderRadius: 4, background: 'transparent', color: 'var(--muted)', cursor: 'pointer',
          }}
        >
          <span style={{
            fontSize: 10, lineHeight: 1,
            transform: isCollapsed ? 'rotate(-90deg)' : 'none',
            transition: 'transform 0.12s ease',
          }}>▼</span>
        </button>
      )}
    </div>
  );
});

function LegendDot({ color, label }: { color: string; label: string }) {
  return (
    <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
      <span style={{ width: 9, height: 9, background: color, borderRadius: 2, display: 'inline-block' }} />
      {label}
    </span>
  );
}

