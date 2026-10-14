import { useState } from 'react';
import { IconCheck, fmtDelta, fmtMs, fmtNum, relativeTime } from '../../../common';
import type { WorkflowVersion } from '../interfaces';
import { change, comparable, defaultPair, deltaColor, fmtPts, fmtRunCost, fmtSeen, previousVersion, ptsColor, versionLabel } from '../utils';

interface VersionsTableProps {
  versions: WorkflowVersion[];
  rangeLabel: string;
  onCompare: (base: string, candidate: string) => void;
}

// [select, Version, First seen, Runs, Cost / run, p95, Error, Tokens / run]
const GRID = '28px minmax(150px,1.3fr) minmax(130px,1fr) 80px 110px 80px 80px 110px';
const MIN_WIDTH = 860;

const numCell = { display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: 2, fontVariantNumeric: 'tabular-nums' } as const;

function Delta({ text, color }: { text: string; color: string }) {
  return <span style={{ fontSize: 12, color }}>{text}</span>;
}

export function VersionsTable({ versions, rangeLabel, onCompare }: VersionsTableProps) {
  // null until the user picks, so the default follows the versions as they load.
  const [picked, setSel] = useState<string[] | null>(null);
  const firstSeen = new Map(versions.filter(comparable).map((v) => [v.version, v.first_seen_ms]));
  const sel = (picked ?? defaultPair(versions) ?? []).filter((id) => firstSeen.has(id));
  const current = versions.find(comparable)?.version;

  const toggle = (id: string) =>
    setSel(sel.includes(id) ? sel.filter((x) => x !== id) : [...sel, id].slice(-2));

  const compare = () => {
    if (sel.length !== 2) return;
    const [base, candidate] = [...sel].sort((a, b) => (firstSeen.get(a) ?? 0) - (firstSeen.get(b) ?? 0));
    onCompare(base, candidate);
  };

  return (
    <div style={{ marginBottom: 44 }}>
      <div
        style={{
          display: 'flex',
          alignItems: 'flex-end',
          justifyContent: 'space-between',
          gap: 16,
          paddingBottom: 14,
          marginBottom: 6,
          borderBottom: '1px solid var(--border)',
          flexWrap: 'wrap',
        }}
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          <span style={{ fontSize: 12, fontWeight: 500, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--muted)' }}>Versions</span>
          <span style={{ fontSize: 17, fontWeight: 600, color: 'var(--foreground)', letterSpacing: '-0.01em' }}>Releases · {rangeLabel}</span>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <span style={{ fontSize: 13, color: 'var(--muted)' }}>{sel.length} of 2 selected</span>
          <button
            onClick={compare}
            disabled={sel.length !== 2}
            style={{
              padding: '7px 14px',
              borderRadius: 8,
              fontSize: 13.5,
              fontWeight: 600,
              border: 'none',
              color: 'var(--accent-contrast)',
              background: sel.length === 2 ? 'var(--accent)' : 'color-mix(in srgb, var(--accent) 35%, transparent)',
            }}
          >
            Compare
          </button>
        </div>
      </div>

      <div style={{ overflowX: 'auto' }}>
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: GRID,
            gap: 12,
            padding: '10px 6px',
            borderBottom: '1px solid var(--border)',
            fontSize: 12,
            color: 'var(--muted)',
            fontWeight: 500,
            textTransform: 'uppercase',
            letterSpacing: '0.06em',
            minWidth: MIN_WIDTH,
          }}
        >
          <span />
          <span>Version</span>
          <span>First seen</span>
          <span style={{ textAlign: 'right' }}>Runs</span>
          <span style={{ textAlign: 'right' }}>Cost / run</span>
          <span style={{ textAlign: 'right' }}>p95</span>
          <span style={{ textAlign: 'right' }}>Error</span>
          <span style={{ textAlign: 'right' }}>Tokens / run</span>
        </div>

        {versions.map((v) => {
          const named = comparable(v);
          const prev = named ? previousVersion(versions, v) : undefined;
          const isSel = sel.includes(v.version);
          const p95 = v.p95_latency_ms;
          const prevP95 = prev?.p95_latency_ms;
          return (
            <div
              key={v.version}
              style={{
                display: 'grid',
                gridTemplateColumns: GRID,
                gap: 12,
                alignItems: 'center',
                padding: '12px 6px',
                minWidth: MIN_WIDTH,
                borderBottom: '1px solid color-mix(in srgb, var(--border) 50%, transparent)',
                background: isSel ? 'color-mix(in srgb, var(--surface-alt) 60%, transparent)' : 'transparent',
              }}
            >
              {named ? (
                <button
                  onClick={() => toggle(v.version)}
                  role="checkbox"
                  aria-checked={isSel}
                  aria-label={`Select ${v.version} for compare`}
                  style={{
                    width: 16,
                    height: 16,
                    borderRadius: 4,
                    padding: 0,
                    display: 'grid',
                    placeItems: 'center',
                    border: `1px solid ${isSel ? 'var(--accent)' : 'var(--border-strong)'}`,
                    background: isSel ? 'var(--accent)' : 'transparent',
                    color: 'var(--accent-contrast)',
                  }}
                >
                  {isSel && <IconCheck size={11} stroke={3} />}
                </button>
              ) : (
                <span />
              )}
              <span style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0, flexWrap: 'wrap' }}>
                <span style={{ fontFamily: 'var(--font-mono)', fontSize: 13.5, fontWeight: 500, color: named ? 'var(--foreground)' : 'var(--muted)' }}>
                  {versionLabel(v.version)}
                </span>
                {v.version === current && <span style={{ fontSize: 12, color: 'var(--accent)' }}>current</span>}
              </span>
              <span style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                <span style={{ fontSize: 13.5, color: 'var(--foreground)' }}>{fmtSeen(v.first_seen_ms)}</span>
                <span style={{ fontSize: 12, color: 'var(--muted)' }}>last run {relativeTime(v.last_seen_ms)}</span>
              </span>
              <span style={{ fontSize: 13.5, textAlign: 'right', color: 'var(--foreground)', fontVariantNumeric: 'tabular-nums' }}>{fmtNum(v.calls)}</span>
              <span style={numCell}>
                <span style={{ fontSize: 13.5, color: 'var(--foreground)' }}>{fmtRunCost(v.cost_per_run)}</span>
                {prev && <Delta text={fmtDelta(change(v.cost_per_run, prev.cost_per_run))} color={deltaColor(change(v.cost_per_run, prev.cost_per_run))} />}
              </span>
              <span style={numCell}>
                <span style={{ fontSize: 13.5, color: 'var(--foreground)' }}>{p95 == null ? '—' : fmtMs(p95)}</span>
                {p95 != null && prevP95 != null && <Delta text={fmtDelta(change(p95, prevP95))} color={deltaColor(change(p95, prevP95))} />}
              </span>
              <span style={numCell}>
                <span style={{ fontSize: 13.5, color: 'var(--foreground)' }}>{(v.error_rate * 100).toFixed(1)}%</span>
                {prev && <Delta text={fmtPts(v.error_rate, prev.error_rate)} color={ptsColor(v.error_rate, prev.error_rate)} />}
              </span>
              <span style={numCell}>
                <span style={{ fontSize: 13.5, color: 'var(--foreground)' }}>{fmtNum(Math.round(v.tokens_per_run))}</span>
                {prev && <Delta text={fmtDelta(change(v.tokens_per_run, prev.tokens_per_run))} color={deltaColor(change(v.tokens_per_run, prev.tokens_per_run))} />}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
