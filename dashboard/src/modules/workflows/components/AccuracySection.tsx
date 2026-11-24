import { useState } from 'react';
import { Centered, Spinner } from '../../../common';
import type { VersionAccuracy } from '../../../common/interfaces';
import { useCorrections, useFieldAccuracy, useStepAccuracy } from '../hooks/useReviews';
import { versionLabel } from '../utils';
import {
  HATCH, LOW_N, axis, byRelease, ciText, compare, heat, isPatch, needText, pctText, ptsText, reviewsNeeded, wilson,
} from '../utils/accuracy';
import type { Change } from '../utils/accuracy';
import type { WorkflowVersion } from '../interfaces';

const heading = { fontSize: 12, fontWeight: 500, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--muted)' } as const;
const colHead = { ...heading, letterSpacing: '0.06em' } as const;
const mono = 'var(--font-mono)';
const linkButton = { padding: 0, border: 'none', background: 'transparent', color: 'var(--foreground)', fontSize: 13, textDecoration: 'underline', cursor: 'pointer', fontFamily: 'inherit' } as const;
const fieldCols = 'minmax(80px,1fr) 48px 72px 110px 84px';

type Mode = 'level' | 'change';
type Order = 'reviewed' | 'weak' | 'change';
const stepLabel = (step: string) => step || 'Whole run';
const changeColor = (r: Change | null) => (r?.significant ? (r.d > 0 ? 'var(--accent)' : 'var(--error)') : 'var(--muted)');

function Seg<T extends string>({ value, options, onChange }: { value: T; options: [T, string][]; onChange: (v: T) => void }) {
  return (
    <div style={{ display: 'flex', background: 'var(--surface-alt)', border: '1px solid var(--border)', borderRadius: 7, padding: 2 }}>
      {options.map(([id, label]) => (
        <button
          key={id}
          type="button"
          aria-pressed={value === id}
          onClick={() => onChange(id)}
          style={{
            padding: '4px 9px', fontSize: 13, fontWeight: 500, borderRadius: 5, whiteSpace: 'nowrap', cursor: 'pointer', fontFamily: 'inherit',
            background: value === id ? 'var(--surface)' : 'transparent', color: value === id ? 'var(--foreground)' : 'var(--muted)',
            border: `1px solid ${value === id ? 'var(--border)' : 'transparent'}`,
          }}
        >
          {label}
        </button>
      ))}
    </div>
  );
}

interface AccuracySectionProps {
  workflow: string;
  /** The version in view; selected first. */
  version: string;
  versions?: WorkflowVersion[];
  onOpenTrace: (id: string) => void;
}

/**
 * Reviewed accuracy of the whole run and each reviewed span, release by release,
 * then the fields and corrections behind the picked cell. Renders nothing until
 * the workflow has reviews.
 */
export function AccuracySection({ workflow, version, versions = [], onOpenTrace }: AccuracySectionProps) {
  const [mode, setMode] = useState<Mode>('level');
  const [patches, setPatches] = useState<'all' | 'minor'>('all');
  const [order, setOrder] = useState<Order>('reviewed');
  const [sel, setSel] = useState<{ step: string; version: string } | null>(null);
  const [field, setField] = useState<string | null>(null);
  const [page, setPage] = useState(1);

  const rows = useStepAccuracy(workflow).data ?? [];
  // Spans most reviewed first, after the whole run.
  const reviewed = new Map<string, number>();
  for (const a of rows) if (a.span) reviewed.set(a.span, (reviewed.get(a.span) ?? 0) + a.reviewed_runs);
  const allSteps = ['', ...[...reviewed].sort((x, y) => y[1] - x[1] || x[0].localeCompare(y[0])).map(([s]) => s)];

  const all = [...new Set(rows.map(a => a.version))].sort(byRelease);
  const list = patches === 'all' ? all : all.filter((v, i) => !isPatch(v) || i === all.length - 1);
  const at = (step: string, v: string): VersionAccuracy | undefined => rows.find(a => a.span === step && a.version === v);
  const prevAt = (step: string, v: string) => {
    for (let i = list.indexOf(v) - 1; i >= 0; i--) {
      const a = at(step, list[i]);
      if (a) return { ...a, version: list[i] };
    }
    return undefined;
  };
  const changeAt = (step: string, v: string) => {
    const a = at(step, v), p = prevAt(step, v);
    return a && p ? compare(p.correct_runs, p.reviewed_runs, a.correct_runs, a.reviewed_runs) : null;
  };

  const selVersion = sel && list.includes(sel.version) ? sel.version : list.includes(version) ? version : list[list.length - 1] ?? '';
  const selStep = sel?.step ?? allSteps.find(s => at(s, selVersion)) ?? '';
  const sa = at(selStep, selVersion), sp = prevAt(selStep, selVersion);
  const sr = changeAt(selStep, selVersion);
  const sn = sa && sp ? reviewsNeeded(sp.correct_runs, sp.reviewed_runs, sa.correct_runs, sa.reviewed_runs) : null;

  const fields = useFieldAccuracy(workflow, selVersion, selStep, list.length > 0).data ?? [];
  const prevFields = useFieldAccuracy(workflow, sp?.version ?? '', selStep, !!sp).data ?? [];
  const worst = field ?? fields.filter(f => f.wrong > 0).sort((a, b) => b.wrong - a.wrong)[0]?.path ?? null;
  const corrections = useCorrections(workflow, selVersion, selStep, worst, page, list.length > 0);
  const pages = corrections.data ? Math.ceil(corrections.data.total / corrections.data.page_size) : 1;

  if (list.length === 0) return null;

  const pick = (step: string, v: string) => { setSel({ step, version: v }); setField(null); setPage(1); };
  const pickField = (path: string) => { setField(path); setPage(1); };

  const shown = allSteps.filter(s => list.some(v => at(s, v)));
  const spans = shown.filter(s => s !== '');
  const sorted = order === 'weak'
    ? [...spans].sort((a, b) => (at(a, selVersion)?.accuracy ?? 2) - (at(b, selVersion)?.accuracy ?? 2))
    : order === 'change'
      ? [...spans].sort((a, b) => Math.abs(changeAt(b, selVersion)?.z ?? 0) - Math.abs(changeAt(a, selVersion)?.z ?? 0))
      : spans;
  // A workflow with only span reviews has no whole-run row.
  const steps = shown.includes('') ? ['', ...sorted] : sorted;
  const dateOf = (v: string) => {
    const ms = versions.find(o => o.version === v)?.first_seen_ms;
    return ms ? new Date(ms).toLocaleDateString(undefined, { month: 'short', day: 'numeric' }) : '';
  };

  return (
    <div style={{ marginBottom: 44 }}>
      <div style={{ display: 'flex', alignItems: 'flex-end', justifyContent: 'space-between', gap: 16, paddingBottom: 14, marginBottom: 16, borderBottom: '1px solid var(--border)', flexWrap: 'wrap' }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          <span style={heading}>Accuracy</span>
          <span style={{ fontSize: 17, fontWeight: 600, color: 'var(--foreground)', letterSpacing: '-0.01em' }}>By step and release</span>
        </div>
        <div style={{ display: 'flex', gap: 14, alignItems: 'center', flexWrap: 'wrap' }}>
          <Seg value={mode} onChange={setMode} options={[['level', 'Accuracy'], ['change', 'Change']]} />
          <Seg value={patches} onChange={setPatches} options={[['all', `All ${all.length} releases`], ['minor', 'Hide patches']]} />
          <Seg value={order} onChange={setOrder} options={[['reviewed', 'Most reviewed'], ['weak', 'Weakest'], ['change', 'Most changed']]} />
        </div>
      </div>

      <div style={{ overflowX: 'auto' }}>
        <div role="grid" aria-label="Accuracy by step and release" style={{ display: 'grid', gridTemplateColumns: `200px repeat(${list.length}, minmax(52px,1fr))`, border: '1px solid var(--border)', borderRadius: 10, overflow: 'hidden' }}>
          <div style={{ ...colHead, padding: '10px 14px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'flex-end' }}>Step</div>
          {list.map(v => (
            <div key={v} style={{ padding: '8px 4px', borderLeft: '1px solid var(--border)', borderBottom: '1px solid var(--border)', display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 1, background: v === selVersion ? 'var(--surface-alt)' : 'transparent' }}>
              <span style={{ fontFamily: mono, fontSize: 12, fontWeight: 500, color: v === version ? 'var(--accent)' : 'var(--foreground)' }}>{versionLabel(v)}</span>
              <span style={{ fontSize: 11, color: 'var(--muted)' }}>{dateOf(v)}</span>
            </div>
          ))}
          {steps.map(step => {
            const isRun = step === '';
            const rule = isRun ? 'var(--border-strong)' : 'var(--border)';
            return [
              <div key={`${step}:label`} style={{ display: 'flex', alignItems: 'center', padding: '0 10px 0 14px', minHeight: 36, borderBottom: `1px solid ${rule}`, background: isRun ? 'var(--surface)' : 'transparent' }}>
                <span style={{ fontFamily: isRun ? 'inherit' : mono, fontSize: 13, fontWeight: isRun ? 600 : 400, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{stepLabel(step)}</span>
              </div>,
              ...list.map(v => {
                const a = at(step, v), selected = step === selStep && v === selVersion;
                const base = { minHeight: 36, padding: 0, border: 'none', borderLeft: '1px solid var(--border)', borderBottom: `1px solid ${rule}`, outline: selected ? '2px solid var(--foreground)' : 'none', outlineOffset: -2, fontSize: 12.5, fontVariantNumeric: 'tabular-nums', fontFamily: 'inherit' } as const;
                if (!a) return <div key={`${step}:${v}`} title={`${stepLabel(step)} has no reviews in ${versionLabel(v)}`} style={base} />;
                const r = changeAt(step, v), p = prevAt(step, v), low = a.reviewed_runs < LOW_N;
                const title = `${versionLabel(v)} · ${pctText(a.accuracy)} (${ciText(a.correct_runs, a.reviewed_runs)}) · n ${a.reviewed_runs}${r && p ? ` · ${ptsText(r.d)} vs ${versionLabel(p.version)}` : ''}`;
                const d = r ? Math.round(r.d * 100) : 0;
                const [txt, bg, color, weight] = mode === 'level'
                  ? [Math.round(a.accuracy * 100), heat(a.accuracy), low ? 'var(--muted)' : 'var(--foreground)', isRun ? 600 : 400]
                  : [r ? `${d > 0 ? '+' : d < 0 ? '−' : ''}${Math.abs(d)}` : '', r?.significant ? `color-mix(in srgb, ${changeColor(r)} 26%, var(--surface))` : 'transparent', r?.significant ? 'var(--foreground)' : 'var(--muted)', r?.significant ? 600 : 400];
                return (
                  <button key={`${step}:${v}`} type="button" aria-label={`${stepLabel(step)} ${versionLabel(v)}`} aria-pressed={selected} title={title} onClick={() => pick(step, v)}
                    style={{ ...base, cursor: 'pointer', display: 'flex', alignItems: 'center', justifyContent: 'center', backgroundColor: bg, backgroundImage: low ? HATCH : 'none', color, fontWeight: weight }}>
                    {txt}
                  </button>
                );
              }),
            ];
          })}
        </div>
      </div>
      <div style={{ display: 'flex', gap: 18, alignItems: 'center', margin: '10px 2px 32px', fontSize: 12, color: 'var(--muted)', flexWrap: 'wrap' }}>
        <span>{mode === 'level' ? '% correct' : 'Points vs previous release; filled when the 95% interval excludes zero'}</span>
        <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
          <span style={{ width: 12, height: 12, borderRadius: 2, backgroundImage: 'repeating-linear-gradient(135deg, rgba(255,255,255,0.18) 0 2px, transparent 2px 6px)', border: '1px solid var(--border-strong)' }} />
          under {LOW_N} reviews
        </span>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 420px), 1fr))', gap: 40, paddingTop: 24, borderTop: '1px solid var(--border)' }}>
        <div>
          <div style={{ fontSize: 15, fontWeight: 600, marginBottom: 12 }}>
            <span style={{ fontFamily: selStep ? mono : 'inherit' }}>{stepLabel(selStep)}</span> <span style={{ color: 'var(--muted)', fontWeight: 400 }}>·</span> <span style={{ fontFamily: mono }}>{versionLabel(selVersion)}</span>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: 'auto 1fr', columnGap: 16, rowGap: 6, fontSize: 13, fontVariantNumeric: 'tabular-nums', marginBottom: 20 }}>
            <span style={{ color: 'var(--muted)' }}>Accuracy</span>
            <span>{sa ? <><span style={{ fontWeight: 600 }}>{pctText(sa.accuracy)}</span> <span style={{ color: 'var(--muted)' }}>{ciText(sa.correct_runs, sa.reviewed_runs)} · n {sa.reviewed_runs}</span></> : '—'}</span>
            <span style={{ color: 'var(--muted)' }}>vs {sp ? versionLabel(sp.version) : '—'}</span>
            <span>{sr ? <><span style={{ fontWeight: 600, color: sr.significant ? changeColor(sr) : 'var(--foreground)' }}>{ptsText(sr.d)}</span> <span style={{ color: 'var(--muted)' }}>CI {ptsText(sr.lo)} to {ptsText(sr.hi)}</span></> : '—'}</span>
            <span style={{ color: 'var(--muted)' }}>To 95%</span>
            <span>{!sn || !sp ? '—' : sn.done ? 'reached' : `+${sn.a} on ${versionLabel(sp.version)}, +${sn.b} on ${versionLabel(selVersion)}`}</span>
          </div>
          <div style={{ display: 'flex', gap: 10 }}>
            <div style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between', height: 120, fontSize: 11, color: 'var(--muted)', textAlign: 'right', width: 30 }}><span>100%</span><span>75%</span><span>50%</span></div>
            <div style={{ flex: 1, display: 'flex', flexDirection: 'column', gap: 4 }}>
              <div style={{ display: 'flex', height: 120, borderTop: '1px solid var(--chart-grid)', borderBottom: '1px solid var(--chart-grid)', background: 'linear-gradient(var(--chart-grid), var(--chart-grid)) 0 50% / 100% 1px no-repeat' }}>
                {list.map(v => {
                  const a = at(selStep, v), on = v === selVersion;
                  const [lo, hi] = a ? wilson(a.correct_runs, a.reviewed_runs) : [0, 0];
                  const r = changeAt(selStep, v);
                  const color = r?.significant ? changeColor(r) : on ? 'var(--foreground)' : 'color-mix(in srgb, var(--foreground) 55%, transparent)';
                  return (
                    <button key={v} type="button" aria-label={`${versionLabel(v)} interval`} onClick={() => pick(selStep, v)} title={a ? `${versionLabel(v)}: ${pctText(a.accuracy)} (${ciText(a.correct_runs, a.reviewed_runs)}), n ${a.reviewed_runs}` : versionLabel(v)}
                      style={{ flex: 1, position: 'relative', padding: 0, border: 'none', cursor: 'pointer', background: on ? 'var(--surface-active)' : 'transparent' }}>
                      {a && (
                        <>
                          <span style={{ position: 'absolute', left: '50%', width: 2, transform: 'translateX(-50%)', bottom: `${axis(lo)}%`, height: `${axis(hi) - axis(lo)}%`, background: color, opacity: 0.45 }} />
                          <span style={{ position: 'absolute', left: '50%', width: 8, height: 8, borderRadius: '50%', transform: 'translate(-50%, 50%)', bottom: `${axis(a.accuracy)}%`, background: a.reviewed_runs < LOW_N ? 'var(--background)' : color, border: `1.5px solid ${color}` }} />
                        </>
                      )}
                    </button>
                  );
                })}
              </div>
              <div style={{ display: 'flex' }}>
                {list.map(v => <span key={v} style={{ flex: 1, textAlign: 'center', fontSize: 10.5, color: 'var(--muted)', fontFamily: mono }}>{versionLabel(v).replace(/^v/, '')}</span>)}
              </div>
            </div>
          </div>
        </div>

        <div style={{ minWidth: 0 }}>
          {fields.length > 0 && (
            <>
              <div style={{ ...colHead, display: 'grid', gridTemplateColumns: fieldCols, gap: 12, padding: '0 12px 8px' }}>
                <span>Field</span><span style={{ textAlign: 'right' }}>{versionLabel(selVersion)}</span><span style={{ textAlign: 'right' }}>vs {sp ? versionLabel(sp.version) : '—'}</span>
                <span style={{ textAlign: 'right', textTransform: 'none', letterSpacing: 0 }}>wrong · missing</span><span style={{ textAlign: 'right' }}>To 95%</span>
              </div>
              <div role="table" aria-label="Field accuracy" style={{ border: '1px solid var(--border)', borderRadius: 8, marginBottom: 20 }}>
                {fields.map((f, i) => {
                  const p = prevFields.find(x => x.path === f.path);
                  const r = p ? compare(p.reviewed - p.wrong, p.reviewed, f.reviewed - f.wrong, f.reviewed) : null;
                  const need = p ? reviewsNeeded(p.reviewed - p.wrong, p.reviewed, f.reviewed - f.wrong, f.reviewed) : null;
                  const share = (n: number) => `${Math.min(100, n / f.reviewed * 300)}%`;
                  return (
                    <button key={f.path} type="button" role="row" aria-pressed={f.path === worst} onClick={() => pickField(f.path)}
                      style={{ display: 'grid', gridTemplateColumns: fieldCols, gap: 12, alignItems: 'center', width: '100%', padding: '9px 12px', border: 'none', borderTop: i ? '1px solid var(--border)' : 'none', background: f.path === worst ? 'var(--surface-alt)' : 'transparent', textAlign: 'left', fontVariantNumeric: 'tabular-nums', fontFamily: 'inherit', color: 'var(--foreground)', cursor: 'pointer' }}>
                      <span role="cell" style={{ fontFamily: mono, fontSize: 13, wordBreak: 'break-word' }}>{f.path}</span>
                      <span role="cell" style={{ fontSize: 13.5, textAlign: 'right' }}>{pctText(f.accuracy)}</span>
                      <span role="cell" style={{ fontSize: 12.5, fontWeight: 600, textAlign: 'right', color: changeColor(r) }}>{r ? ptsText(r.d) : '—'}</span>
                      <span role="cell" style={{ display: 'flex', alignItems: 'center', gap: 8, justifyContent: 'flex-end' }}>
                        <span style={{ display: 'flex', width: 60, height: 6, borderRadius: 3, overflow: 'hidden', background: 'var(--surface-alt)' }}>
                          <span style={{ width: share(f.wrong - f.missing), background: 'var(--error)' }} />
                          <span style={{ width: share(f.missing), background: 'var(--warning)' }} />
                        </span>
                        <span style={{ fontSize: 12.5, color: 'var(--muted)' }}>{f.wrong - f.missing} · {f.missing}</span>
                      </span>
                      <span role="cell" style={{ fontSize: 12.5, textAlign: 'right', color: need?.done ? 'var(--accent)' : 'var(--foreground)' }}>{needText(need)}</span>
                    </button>
                  );
                })}
              </div>
            </>
          )}

          <div style={{ ...heading, marginBottom: 8 }}>
            Corrections{worst && <> · <span style={{ fontFamily: mono, textTransform: 'none' }}>{worst}</span></>}
          </div>
          {corrections.isLoading ? <Centered minHeight={80}><Spinner /></Centered>
            : corrections.isError ? <Centered minHeight={80}>Couldn't load corrections</Centered>
            : !corrections.data?.items.length ? <Centered minHeight={80}>No corrections for this step in this release.</Centered>
            : (
              <div style={{ border: '1px solid var(--border)', borderRadius: 8 }}>
                {corrections.data.items.map((r, i) => {
                  const f = worst ? r.fields.find(x => x.path === worst) : undefined;
                  const kind = f ? (f.missing ? 'Missing' : !f.corrected ? 'Extra' : 'Wrong value') : r.verdict === 'missing' ? 'Missing output' : 'Wrong';
                  const out = f ? f.output || 'missing' : r.output;
                  const fix = f ? f.corrected || 'removed' : r.corrected_output ?? '';
                  return (
                    <div key={`${r.trace_id}:${r.span_id}`} style={{ display: 'grid', gridTemplateColumns: '110px 90px minmax(0,1fr)', gap: 12, alignItems: 'center', padding: '8px 12px', borderTop: i ? '1px solid var(--border)' : 'none', fontSize: 13 }}>
                      <button type="button" onClick={() => onOpenTrace(r.trace_id)} title={r.trace_id} style={{ ...linkButton, fontFamily: mono, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', textAlign: 'left' }}>{r.trace_id}</button>
                      <span style={{ fontSize: 12, color: 'var(--muted)' }}>{kind}</span>
                      <span title={r.note || undefined} style={{ fontFamily: mono, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        <del style={{ color: 'var(--muted)' }}>{out}</del><span style={{ color: 'var(--muted)', margin: '0 6px' }}>→</span>{fix}
                      </span>
                    </div>
                  );
                })}
              </div>
            )}
          {pages > 1 && (
            <div style={{ display: 'flex', gap: 12, alignItems: 'center', marginTop: 10, fontSize: 13, color: 'var(--muted)' }}>
              <button type="button" style={linkButton} disabled={page <= 1} onClick={() => setPage(p => p - 1)}>Previous</button>
              <span>Page {page} of {pages}</span>
              <button type="button" style={linkButton} disabled={page >= pages} onClick={() => setPage(p => p + 1)}>Next</button>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
