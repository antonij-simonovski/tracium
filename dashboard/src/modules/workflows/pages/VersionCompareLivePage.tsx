import { useState } from 'react';
import {
  Centered,
  DELTA_COLOR,
  EmptyState,
  Spinner,
  bucketLabel,
  fmtCost,
  fmtDelta,
  fmtMs,
  fmtNum,
  isLongRange,
  rangeLabel,
} from '../../../common';
import hover from '../../../common/styles/hover.module.css';
import { APIError } from '../../../common/api/client';
import {
  useVersionComparison,
  useWorkflowCostSeries,
  useWorkflowErrorSeries,
  useWorkflowLatencySeries,
  useWorkflowVersions,
} from '../hooks/useWorkflowDetail';
import { TrafficStrip } from '../components/TrafficStrip';
import { VersionDropdown } from '../components/VersionDropdown';
import { VersionLineChart } from '../components/VersionLineChart';
import type { Kpi } from '../../overview/interfaces';
import type { WorkflowVersion } from '../interfaces';
import { change, fmtPts, fmtRunCost, fmtSeen, overlap, ptsColor, releaseMarkers } from '../utils';

type MetricId = 'runs' | 'cost' | 'cpr' | 'p95' | 'err' | 'tpr';

interface Row {
  id: MetricId;
  label: string;
  title: string;
  a: number | null;
  b: number | null;
  fmt: (v: number) => string;
  delta: string;
  color: string;
  chartable: boolean;
}

interface VersionCompareLivePageProps {
  workflowName: string;
  base: string;
  candidate: string;
  range: string;
  onSelect: (base: string, candidate: string) => void;
}

const Y_FMT: Record<MetricId, (v: number) => string> = {
  runs: (v) => fmtNum(Math.round(v)),
  cost: fmtCost,
  cpr: fmtRunCost,
  p95: fmtMs,
  err: (v) => `${(v * 100).toFixed(1)}%`,
  tpr: (v) => fmtNum(Math.round(v)),
};

// The API marks a change neutral when the base has no value to compare against.
const kpiDelta = (k: Kpi) => ({
  delta: k.delta_type === 'neutral' ? '—' : fmtDelta(k.delta),
  color: DELTA_COLOR[k.delta_type],
});

const eyebrow = { fontSize: 12, fontWeight: 500, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--muted)' } as const;

function BandLegend({ label }: { label: string }) {
  return (
    <span style={{ whiteSpace: 'nowrap' }}>
      <span
        style={{
          display: 'inline-block',
          width: 12,
          height: 9,
          borderRadius: 2,
          verticalAlign: 'middle',
          marginRight: 5,
          background: 'color-mix(in srgb, var(--info) 22%, transparent)',
          border: '1px dashed color-mix(in srgb, var(--info) 55%, transparent)',
        }}
      />
      {label}
    </span>
  );
}

function Legend({ color, opacity = 1, label }: { color: string; opacity?: number; label: string }) {
  return (
    <span style={{ whiteSpace: 'nowrap' }}>
      <span style={{ display: 'inline-block', width: 10, height: 2, background: color, opacity, verticalAlign: 'middle', marginRight: 5 }} />
      {label}
    </span>
  );
}

interface VersionCellProps {
  v: WorkflowVersion;
  label: string;
  versions: WorkflowVersion[];
  exclude: string;
  onChange: (version: string) => void;
  color: string;
  opacity?: number;
  divided?: boolean;
}

function VersionCell({ v, label, versions, exclude, onChange, color, opacity, divided }: VersionCellProps) {
  return (
    <div style={{ padding: '14px 22px 16px', display: 'flex', flexDirection: 'column', gap: 6, minWidth: 0, borderLeft: divided ? '1px solid var(--border)' : 'none' }}>
      <span style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
        <span style={{ width: 14, height: 2, background: color, opacity, flexShrink: 0 }} />
        <VersionDropdown
          label={label}
          value={v.version}
          options={versions.filter((o) => o.version && o.version !== exclude).map((o) => ({ id: o.version, label: o.version, calls: o.calls }))}
          onChange={(id) => id && onChange(id)}
          trigger={<span style={{ fontFamily: 'var(--font-mono)', fontSize: 15, fontWeight: 500, color: 'var(--foreground)', whiteSpace: 'nowrap' }}>{v.version}</span>}
          triggerLabel={`${label}: ${v.version}`}
          triggerStyle={{ gap: 6, padding: '3px 8px', margin: '-4px -9px', background: 'transparent', border: '1px solid transparent' }}
          popoverStyle={{ top: 'calc(100% + 10px)', left: -9 }}
          emptyText="No other versions in this range"
        />
      </span>
      <span style={{ fontSize: 12.5, color: 'var(--muted)' }}>
        {fmtSeen(v.first_seen_ms)} – {fmtSeen(v.last_seen_ms)}
      </span>
    </div>
  );
}

export function VersionCompareLivePage({ workflowName, base, candidate, range, onSelect }: VersionCompareLivePageProps) {
  const [metric, setMetric] = useState<MetricId>('cpr');
  const long = isLongRange(range);
  const cmp = useVersionComparison(workflowName, base, candidate, range);
  const versions = useWorkflowVersions(workflowName, range).data?.items ?? [];

  // Version-scoped series are raw-window only; the error series doubles as the
  // per-bucket run count.
  const needCost = !long && (metric === 'cost' || metric === 'cpr');
  const needLatency = !long && metric === 'p95';
  const errAll = useWorkflowErrorSeries(workflowName, range, !long);
  const errA = useWorkflowErrorSeries(workflowName, range, !long, base);
  const errB = useWorkflowErrorSeries(workflowName, range, !long, candidate);
  const costA = useWorkflowCostSeries(workflowName, range, needCost, base);
  const costB = useWorkflowCostSeries(workflowName, range, needCost, candidate);
  const latA = useWorkflowLatencySeries(workflowName, range, needLatency, base);
  const latB = useWorkflowLatencySeries(workflowName, range, needLatency, candidate);

  if (cmp.isLoading) {
    return (
      <Centered>
        <Spinner />
      </Centered>
    );
  }
  if (!cmp.data) {
    const missing = cmp.error instanceof APIError && cmp.error.isNotFound;
    return missing ? (
      <EmptyState
        message="Nothing to compare in this range"
        description={`${base} or ${candidate} has no runs in the ${rangeLabel(range)}. Try a longer range.`}
      />
    ) : (
      <Centered>Failed to load the comparison</Centered>
    );
  }

  const { base: A, candidate: B } = cmp.data;
  const rows: Row[] = [
    { id: 'runs', label: 'Runs', title: 'Runs', a: A.calls, b: B.calls, fmt: Y_FMT.runs, delta: fmtDelta(change(B.calls, A.calls)), color: 'var(--muted)', chartable: true },
    { id: 'cost', label: 'Total cost', title: 'Cost', a: A.cost, b: B.cost, fmt: fmtCost, delta: fmtDelta(change(B.cost, A.cost)), color: 'var(--muted)', chartable: true },
    { id: 'cpr', label: 'Cost / run', title: 'Cost per run', a: A.cost_per_run, b: B.cost_per_run, fmt: fmtRunCost, ...kpiDelta(cmp.data.cost_per_run), chartable: true },
    { id: 'p95', label: 'p95 latency', title: 'p95 latency', a: A.p95_latency_ms, b: B.p95_latency_ms, fmt: fmtMs, ...kpiDelta(cmp.data.p95_latency_ms), chartable: true },
    { id: 'err', label: 'Error rate', title: 'Error rate', a: A.error_rate, b: B.error_rate, fmt: Y_FMT.err, delta: fmtPts(B.error_rate, A.error_rate), color: ptsColor(B.error_rate, A.error_rate), chartable: true },
    { id: 'tpr', label: 'Tokens / run', title: 'Tokens per run', a: A.tokens_per_run, b: B.tokens_per_run, fmt: Y_FMT.tpr, ...kpiDelta(cmp.data.tokens_per_run), chartable: false },
  ];
  const active = rows.find((r) => r.id === metric)!;

  const axis = errAll.data?.items ?? [];
  const bucketsMs = axis.map((b) => b.bucket_ms);
  const labels = bucketsMs.map((ms) => bucketLabel(ms, range));
  const byBucket = <T extends { bucket_ms: number }>(items: T[] | undefined) => new Map((items ?? []).map((b) => [b.bucket_ms, b]));
  const errAMap = byBucket(errA.data?.items);
  const errBMap = byBucket(errB.data?.items);

  const seriesFor = (errs: typeof errAMap, cost: typeof costA, lat: typeof latA): (number | null)[] => {
    const costs = byBucket(cost.data?.items);
    const lats = byBucket(lat.data?.items);
    return bucketsMs.map((ms) => {
      const e = errs.get(ms);
      if (!e || e.total === 0) return null;
      switch (metric) {
        case 'runs': return e.total;
        case 'err': return e.errors / e.total;
        case 'cost': return costs.get(ms)?.value ?? null;
        case 'cpr': { const c = costs.get(ms); return c ? c.value / e.total : null; }
        case 'p95': return lats.get(ms)?.p95 ?? null;
        default: return null;
      }
    });
  };
  const sA = seriesFor(errAMap, costA, latA);
  const sB = seriesFor(errBMap, costB, latB);

  const both = bucketsMs.map((ms, i) => ((errAMap.get(ms)?.total ?? 0) > 0 && (errBMap.get(ms)?.total ?? 0) > 0 ? i : -1)).filter((i) => i >= 0);
  const ov = overlap(A, B);
  // A bucket can hold both versions back to back; shade only a real overlap.
  const band: [number, number] | null = ov && both.length ? [both[0], both[both.length - 1]] : null;
  const markers = releaseMarkers([A, B], bucketsMs, (v) => (v.version === B.version ? 'var(--accent)' : 'var(--muted-foreground)'));

  const traffic = axis.map((b) => ({
    label: bucketLabel(b.bucket_ms, range),
    base: errAMap.get(b.bucket_ms)?.total ?? 0,
    candidate: errBMap.get(b.bucket_ms)?.total ?? 0,
    total: b.total,
  }));

  const chartLoading = errA.isLoading || errB.isLoading || (needCost && (costA.isLoading || costB.isLoading)) || (needLatency && (latA.isLoading || latB.isLoading));
  const chartFailed = errAll.isError || errA.isError || errB.isError || costA.isError || costB.isError || latA.isError || latB.isError;
  const noRuns = sA.every((v) => v == null) && sB.every((v) => v == null);
  const showBand = !long && active.chartable && !chartFailed && !chartLoading && !noRuns && band != null;
  const chart = long
    ? <Centered minHeight={240}>Over-time charts need a range of 30 days or less.</Centered>
    : !active.chartable
      ? <Centered minHeight={240}>Tokens per run isn't charted over time.</Centered>
      : chartFailed
        ? <Centered minHeight={240}>Couldn't load the series</Centered>
        : chartLoading
          ? <Centered minHeight={240}><Spinner /></Centered>
          : noRuns
            ? <Centered minHeight={240}>No runs in this range.</Centered>
            : (
              <VersionLineChart
                labels={labels}
                base={sA}
                candidate={sB}
                baseLabel={A.version}
                candidateLabel={B.version}
                yFmt={Y_FMT[metric]}
                band={band}
                markers={markers}
              />
            );

  return (
    <div style={{ padding: '32px 40px 64px', maxWidth: 1480, margin: '0 auto' }}>
      <div style={{ display: 'flex', alignItems: 'flex-end', justifyContent: 'space-between', gap: 20, marginBottom: 22, flexWrap: 'wrap' }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6, minWidth: 0 }}>
          <span style={{ ...eyebrow, letterSpacing: '0.1em' }}>{workflowName} · Compare versions</span>
          <h1 style={{ fontSize: 26, fontWeight: 600, letterSpacing: '-0.02em', margin: 0, fontFamily: 'var(--font-mono)', overflowWrap: 'anywhere' }}>
            {A.version} <span style={{ color: 'var(--muted)', fontFamily: 'inherit', fontWeight: 400 }}>vs</span> {B.version}
          </h1>
        </div>
        <button
          onClick={() => onSelect(candidate, base)}
          className={hover.outlined}
          style={{ whiteSpace: 'nowrap', padding: '5px 10px', background: 'var(--surface-alt)', border: '1px solid var(--border)', borderRadius: 7, fontSize: 13 }}
        >
          Swap
        </button>
      </div>

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))',
          borderTop: '1px solid var(--border)',
          borderBottom: '1px solid var(--border)',
          marginBottom: 28,
        }}
      >
        <VersionCell v={A} label="Base" versions={versions} exclude={B.version} onChange={(v) => onSelect(v, candidate)} color="var(--foreground)" opacity={0.5} />
        <VersionCell v={B} label="Candidate" versions={versions} exclude={A.version} onChange={(v) => onSelect(base, v)} color="var(--accent)" divided />
      </div>

      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '28px 40px', alignItems: 'flex-start' }}>
        <div style={{ flex: '1 1 340px', maxWidth: 520 }}>
          <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0,1.35fr) minmax(max-content,1fr) minmax(max-content,1fr) 70px', columnGap: 12 }}>
            <div
              style={{
                display: 'grid',
                gridColumn: '1 / -1',
                gridTemplateColumns: 'subgrid',
                padding: '0 10px 10px',
                borderBottom: '1px solid var(--border)',
                ...eyebrow,
                letterSpacing: '0.06em',
              }}
            >
              <span>Metric</span>
              <span style={{ textAlign: 'right', textTransform: 'none', letterSpacing: 0, fontFamily: 'var(--font-mono)', whiteSpace: 'nowrap' }}>{A.version}</span>
              <span style={{ textAlign: 'right', textTransform: 'none', letterSpacing: 0, fontFamily: 'var(--font-mono)', color: 'var(--foreground)', whiteSpace: 'nowrap' }}>{B.version}</span>
              <span style={{ textAlign: 'right' }}>Change</span>
            </div>
            {rows.map((r) => {
              const on = r.id === metric;
              return (
                <button
                  key={r.id}
                  onClick={() => setMetric(r.id)}
                  aria-pressed={on}
                  className={hover.row}
                  style={{
                    display: 'grid',
                    gridColumn: '1 / -1',
                    gridTemplateColumns: 'subgrid',
                    alignItems: 'center',
                    width: '100%',
                    padding: '11px 10px',
                    border: 'none',
                    borderRadius: 6,
                    textAlign: 'left',
                    background: on ? 'var(--surface-alt)' : undefined,
                    borderBottom: '1px solid color-mix(in srgb, var(--border) 50%, transparent)',
                  }}
                >
                  <span style={{ fontSize: 13.5, color: on ? 'var(--foreground)' : 'var(--muted)', fontWeight: on ? 500 : 400, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                    {r.label}
                  </span>
                  <span style={{ fontSize: 13.5, textAlign: 'right', color: 'var(--muted)', fontVariantNumeric: 'tabular-nums', whiteSpace: 'nowrap' }}>
                    {r.a == null ? '—' : r.fmt(r.a)}
                  </span>
                  <span style={{ fontSize: 13.5, textAlign: 'right', color: 'var(--foreground)', fontWeight: 500, fontVariantNumeric: 'tabular-nums', whiteSpace: 'nowrap' }}>
                    {r.b == null ? '—' : r.fmt(r.b)}
                  </span>
                  <span
                    style={{
                      justifySelf: 'end',
                      fontSize: 12.5,
                      fontWeight: 600,
                      padding: '1px 7px',
                      borderRadius: 999,
                      color: r.color,
                      background: `color-mix(in srgb, ${r.color} 12%, transparent)`,
                      fontVariantNumeric: 'tabular-nums',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {r.delta}
                  </span>
                </button>
              );
            })}
          </div>
          <p style={{ fontSize: 12.5, color: 'var(--muted)', margin: '12px 10px 0' }}>Totals over the {rangeLabel(range)}. Click a row to chart it.</p>
        </div>

        <div style={{ flex: '3 1 440px', minWidth: 0 }}>
          <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: '8px 12px', marginBottom: 14, flexWrap: 'wrap' }}>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
              <span style={eyebrow}>Over time</span>
              <span style={{ fontSize: 15, fontWeight: 600, color: 'var(--foreground)', letterSpacing: '-0.01em' }}>
                {active.title} · {range === '24h' ? 'hourly' : 'daily'}, {rangeLabel(range)}
              </span>
            </div>
            <div style={{ display: 'flex', gap: 12, fontSize: 12.5, color: 'var(--muted)', fontFamily: 'var(--font-mono)', flexWrap: 'wrap' }}>
              <Legend color="var(--foreground)" opacity={0.5} label={A.version} />
              <Legend color="var(--accent)" label={B.version} />
              {showBand && ov && <BandLegend label={`Concurrent ${fmtSeen(ov[0])} – ${fmtSeen(ov[1])}`} />}
            </div>
          </div>
          <div style={{ paddingTop: 10 }}>{chart}</div>
          {!long && traffic.length > 0 && (
            <>
              <div style={{ ...eyebrow, margin: '16px 0 8px' }}>Share of runs</div>
              <TrafficStrip buckets={traffic} baseLabel={A.version} candidateLabel={B.version} />
            </>
          )}
        </div>
      </div>
    </div>
  );
}
