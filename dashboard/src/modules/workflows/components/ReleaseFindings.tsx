import { fmtDelta, fmtMs, fmtNum } from '../../../common';
import type { ReleaseReport } from '../interfaces';
import { change, fmtPts, fmtRunCost } from '../utils';
import { VerdictBadge } from './VerdictBadge';

const RATES = new Set(['error_rate', 'truncation_rate', 'refusal_rate', 'tool_error_rate', 'cache_hit_ratio']);

const LABELS: Record<string, string> = {
  error_rate: 'Error rate',
  truncation_rate: 'Truncated LLM calls',
  refusal_rate: 'Refused LLM calls',
  tool_error_rate: 'Tool errors',
  cost_per_run: 'Cost / run (median)',
  latency_p95: 'p95 latency',
  llm_calls_per_run: 'LLM calls / run (median)',
  output_tokens_per_run: 'Output tokens / run (median)',
  cache_hit_ratio: 'Cache hit ratio',
};

const VERDICT_COLOR = { regressed: 'var(--error)', improved: 'var(--accent)', same: 'var(--muted)' } as const;

function fmtValue(metric: string, v: number): string {
  if (RATES.has(metric)) return `${(v * 100).toFixed(1)}%`;
  if (metric === 'cost_per_run') return fmtRunCost(v);
  if (metric === 'latency_p95') return fmtMs(v);
  return fmtNum(Math.round(v));
}

const cell = { fontSize: 13.5, textAlign: 'right', fontVariantNumeric: 'tabular-nums', whiteSpace: 'nowrap' } as const;

// The release report's findings for the version against the one it replaced.
export function ReleaseFindings({ report }: { report: ReleaseReport }) {
  return (
    <div style={{ marginBottom: 28 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 10, flexWrap: 'wrap' }}>
        <span style={{ fontSize: 12, fontWeight: 500, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--muted)' }}>Release report</span>
        <VerdictBadge verdict={report.status === 'pending' ? 'pending' : report.verdict} />
        <span style={{ fontSize: 12.5, color: 'var(--muted)' }}>
          {fmtNum(report.runs)} runs of {report.version} against {fmtNum(report.prev_runs)} of {report.prev_version}
        </span>
      </div>
      {report.findings.length > 0 && (
        <div style={{ overflowX: 'auto' }}>
          <div style={{ display: 'grid', gridTemplateColumns: 'minmax(180px,1.5fr) repeat(3, minmax(90px,1fr)) 100px', columnGap: 12, minWidth: 560 }}>
            {['Metric', report.prev_version, report.version, 'Change', 'Verdict'].map((h, i) => (
              <span key={i} style={{ padding: '0 0 8px', borderBottom: '1px solid var(--border)', fontSize: 12, color: 'var(--muted)', textAlign: i ? 'right' : 'left', fontFamily: i === 1 || i === 2 ? 'var(--font-mono)' : undefined }}>
                {h}
              </span>
            ))}
            {report.findings.map((f) => (
              <div key={f.metric} style={{ display: 'contents' }}>
                <span style={{ fontSize: 13.5, padding: '9px 0', color: 'var(--foreground)' }}>{LABELS[f.metric] ?? f.metric}</span>
                <span style={{ ...cell, padding: '9px 0', color: 'var(--muted)' }}>{fmtValue(f.metric, f.prev)}</span>
                <span style={{ ...cell, padding: '9px 0', color: 'var(--foreground)', fontWeight: 500 }}>{fmtValue(f.metric, f.next)}</span>
                <span style={{ ...cell, padding: '9px 0', color: 'var(--muted)' }}>{RATES.has(f.metric) ? fmtPts(f.next, f.prev) : fmtDelta(change(f.next, f.prev))}</span>
                <span style={{ ...cell, padding: '9px 0', color: VERDICT_COLOR[f.verdict] }}>{f.verdict}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
