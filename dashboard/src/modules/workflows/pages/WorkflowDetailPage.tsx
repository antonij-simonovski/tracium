import {
  Centered,
  CostBarChart,
  LatencyChart,
  HorizonStrip,
  MetaRow,
  RunsTable,
  StatTile,
  fmtCost,
  fmtNum,
  fmtPct,
  plural,
  rangeLabel,
  SetupIssueNote,
} from '../../../common';
import type { MetaRowProps, RunRow, StatTone } from '../../../common';
import type { CostPoint, LatencyPoint, ErrorPoint, ChartMarker } from '../../../common/interfaces';
import hover from '../../../common/styles/hover.module.css';
import { VersionDropdown } from '../components/VersionDropdown';
import { VersionsTable } from '../components/VersionsTable';
import type { WorkflowVersion } from '../interfaces';
import { ERR_BAD, ERR_WARN, fmtRunCost } from '../utils';
import styles from './WorkflowDetailPage.module.css';

export interface WorkflowDetailPageProps {
  name: string;
  version?: string;
  versionSince?: string;
  description?: string;
  range: string;
  // Narrows the tiles' window label, e.g. to one version.
  scope?: string;

  calls: number;
  completed: number;
  failed: number;
  cost: number;
  p95Ms: number | null;
  errorRate: number;
  configRows: MetaRowProps[];

  costSeries: CostPoint[];
  latencySeries: LatencyPoint[];
  errorSeries: ErrorPoint[];
  runs: RunRow[];
  markers?: ChartMarker[];
  // Present once the workflow has versions; drives the filter, table and compare.
  versions?: WorkflowVersion[];
  versionFilter?: string | null;
  onVersionFilter?: (version: string | null) => void;
  onCompare?: (base: string, candidate: string) => void;
  defaultCompare?: [string, string] | null;
  costAlert?: { version: string; from: number; to: number; change: string };
  loadErrors?: Partial<Record<'cost' | 'latency' | 'errors' | 'runs', boolean>>;

  setView: (v: string) => void;
  setSelected: (updater: (prev: Record<string, string>) => Record<string, string>) => void;
}

const linkButton = {
  padding: 0,
  border: 'none',
  background: 'transparent',
  color: 'var(--foreground)',
  fontSize: 'inherit',
  textDecoration: 'underline',
} as const;

export function WorkflowDetailPage(props: WorkflowDetailPageProps) {
  const {
    name, version, versionSince, description, range, scope,
    calls, completed, failed, cost, p95Ms, errorRate,
    configRows, costSeries, latencySeries, errorSeries, runs, markers = [], loadErrors = {},
    versions, versionFilter = null, onVersionFilter, onCompare, defaultCompare, costAlert,
    setView, setSelected,
  } = props;

  const errTone: StatTone = errorRate > ERR_BAD ? 'bad' : errorRate > ERR_WARN ? 'warn' : 'good';
  const windowLabel = rangeLabel(range);
  const hourly = range === '24h';

  const openTrace = (id: string) => {
    setSelected((s) => ({ ...s, traceId: id }));
    setView('trace');
  };

  return (
    <div style={{ padding: '32px 40px 64px', maxWidth: 1480, margin: '0 auto' }}>
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 24, marginBottom: 22, flexWrap: 'wrap' }}>
        <div style={{ minWidth: 0 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12, flexWrap: 'wrap' }}>
            <h1 style={{ fontSize: 26, fontWeight: 600, letterSpacing: '-0.02em', margin: 0 }}>{name}</h1>
            {version && (
              <span
                style={{
                  fontSize: 12.5,
                  padding: '3px 9px',
                  borderRadius: 6,
                  background: 'var(--surface-alt)',
                  border: '1px solid var(--border-strong)',
                  color: 'var(--foreground)',
                  fontFamily: 'var(--font-mono)',
                }}
              >
                {version}
              </span>
            )}
            {version && versionSince && <span style={{ fontSize: 13, color: 'var(--muted)' }}>current since {versionSince}</span>}
          </div>
          {description && (
            <p style={{ fontSize: 14.5, color: 'var(--muted)', margin: 0, maxWidth: 660, lineHeight: 1.55, textWrap: 'pretty' }}>
              {description}
            </p>
          )}
        </div>
        {versions && versions.length > 0 && onVersionFilter && (
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <VersionDropdown
              label="Filter by version"
              value={versionFilter}
              options={[
                { id: null, label: 'All versions', calls: versions.reduce((s, v) => s + v.calls, 0) },
                ...versions.filter((v) => v.version).map((v) => ({ id: v.version, label: v.version, calls: v.calls })),
              ]}
              onChange={onVersionFilter}
              trigger={
                <>
                  <span>Version</span>
                  <span style={{ fontFamily: 'var(--font-mono)', fontSize: 12.5, color: 'var(--foreground)' }}>{versionFilter ?? 'All versions'}</span>
                </>
              }
              triggerStyle={{ gap: 8, padding: '6px 10px', background: 'var(--surface-alt)', border: '1px solid var(--border)', fontSize: 13 }}
              popoverStyle={{ top: 'calc(100% + 6px)', right: 0 }}
            />
            {onCompare && defaultCompare && (
              <button
                onClick={() => onCompare(...defaultCompare)}
                className={hover.outlined}
                style={{ padding: '6px 12px', background: 'var(--surface-alt)', border: '1px solid var(--border)', borderRadius: 7, fontSize: 13 }}
              >
                Compare versions
              </button>
            )}
          </div>
        )}
      </div>

      {costAlert && (
        <div style={{ marginBottom: 28 }}>
          <SetupIssueNote issue={{ code: 'version_cost', severity: 'warning', message: `Cost per run up since ${costAlert.version}` }}>
            <span style={{ fontVariantNumeric: 'tabular-nums' }}>
              {fmtRunCost(costAlert.from)} → {fmtRunCost(costAlert.to)} ({costAlert.change})
            </span>
            {onCompare && defaultCompare && (
              <button onClick={() => onCompare(...defaultCompare)} style={linkButton}>
                Compare
              </button>
            )}
          </SetupIssueNote>
        </div>
      )}

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(6, 1fr)',
          borderTop: '1px solid var(--border)',
          borderBottom: '1px solid var(--border)',
          margin: '4px 0 32px',
        }}
      >
        <StatTile isFirst label="Total runs" value={fmtNum(calls)} sub={scope ? `${windowLabel} · ${scope}` : windowLabel} />
        <StatTile label="Completed" value={fmtNum(completed)} />
        <StatTile label="Failed" value={fmtNum(failed)} tone={failed > 0 ? errTone : undefined} />
        <StatTile label="Success rate" value={calls > 0 ? fmtPct((completed / calls) * 100) : '—'} tone={calls > 0 ? errTone : undefined} />
        <StatTile label="Total cost" value={fmtCost(cost)} sub={calls > 0 ? `${fmtCost(cost / calls)} / run avg` : undefined} />
        <StatTile label="p95 latency" value={p95Ms == null ? '—' : `${(p95Ms / 1000).toFixed(1)}s`} tone={p95Ms != null && p95Ms > 8000 ? 'warn' : undefined} />
      </div>

      <div
        className={styles.charts}
        style={{ borderTop: '1px solid var(--border)', borderBottom: '1px solid var(--border)', marginBottom: 44 }}
      >
        <div style={{ padding: '22px 26px 20px', minWidth: 0 }}>
          <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 12, marginBottom: 18 }}>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
              <span style={{ fontSize: 12, fontWeight: 500, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--muted)' }}>Spend</span>
              <span style={{ fontSize: 15, fontWeight: 600, color: 'var(--foreground)', letterSpacing: '-0.01em' }}>{hourly ? 'Hourly' : 'Daily'} cost · {windowLabel}</span>
            </div>
            <span style={{ fontSize: 13, color: 'var(--muted)', fontVariantNumeric: 'tabular-nums' }}>{fmtCost(cost)} total</span>
          </div>
          {loadErrors.cost ? <Centered minHeight={210}>Couldn't load spend</Centered> : <CostBarChart series={costSeries} height={210} markers={markers} />}
        </div>
        <div className={styles.divided} style={{ padding: '22px 26px 20px', minWidth: 0 }}>
          <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 12, marginBottom: 18 }}>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
              <span style={{ fontSize: 12, fontWeight: 500, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--muted)' }}>Latency</span>
              <span style={{ fontSize: 15, fontWeight: 600, color: 'var(--foreground)', letterSpacing: '-0.01em' }}>Response time · p50 / p95 / p99</span>
            </div>
            <div style={{ display: 'flex', gap: 12, fontSize: 12.5, color: 'var(--muted)', fontFamily: 'var(--font-mono)' }}>
              <span><span style={{ display: 'inline-block', width: 8, height: 2, background: 'var(--foreground)', opacity: 0.4, verticalAlign: 'middle', marginRight: 4 }} />p50</span>
              <span><span style={{ display: 'inline-block', width: 8, height: 2, background: 'var(--accent)', verticalAlign: 'middle', marginRight: 4 }} />p95</span>
              <span><span style={{ display: 'inline-block', width: 8, height: 2, background: 'var(--warning)', verticalAlign: 'middle', marginRight: 4 }} />p99</span>
            </div>
          </div>
          {loadErrors.latency ? <Centered minHeight={210}>Couldn't load latency</Centered> : <LatencyChart series={latencySeries} height={210} markers={markers} />}
        </div>
      </div>

      {versions && versions.length > 0 && onCompare && (
        <VersionsTable versions={versions} rangeLabel={windowLabel} onCompare={onCompare} />
      )}

      <div style={{ marginBottom: 44 }}>
        <div
          style={{
            display: 'flex',
            alignItems: 'flex-end',
            justifyContent: 'space-between',
            gap: 16,
            paddingBottom: 14,
            marginBottom: 22,
            borderBottom: '1px solid var(--border)',
          }}
        >
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <span style={{ fontSize: 12, fontWeight: 500, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--muted)' }}>Reliability</span>
            <span style={{ fontSize: 17, fontWeight: 600, color: 'var(--foreground)', letterSpacing: '-0.01em' }}>Failed runs</span>
            <span style={{ fontSize: 13.5, color: 'var(--muted)' }}>Errors per {hourly ? 'hour' : 'day'} · {windowLabel}</span>
          </div>
          <span style={{ fontSize: 13, color: errorRate > ERR_BAD ? 'var(--error)' : 'var(--muted)', fontVariantNumeric: 'tabular-nums' }}>
            {plural(failed, 'failure')}
          </span>
        </div>
        {loadErrors.errors ? <Centered minHeight={44}>Couldn't load errors</Centered> : <HorizonStrip data={errorSeries} height={44} />}
      </div>

      <div className={styles.split}>
        <div style={{ minWidth: 0 }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, margin: '0 0 14px' }}>
            <h2 style={{ fontSize: 17, fontWeight: 600, letterSpacing: '-0.01em', margin: 0 }}>Recent runs</h2>
            {versionFilter && (
              <span style={{ fontSize: 13, color: 'var(--muted)' }}>
                Version: <span style={{ fontFamily: 'var(--font-mono)', fontSize: 12.5, color: 'var(--foreground)' }}>{versionFilter}</span>
              </span>
            )}
          </div>
          {loadErrors.runs ? <Centered minHeight={120}>Couldn't load runs</Centered> : <RunsTable runs={runs} onOpen={openTrace} showVersion={!!versions?.length} />}
        </div>

        <div>
          <div style={{ fontSize: 12, fontWeight: 500, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--muted)', marginBottom: 8 }}>
            Configuration
          </div>
          {configRows.map((row) => (
            <MetaRow key={row.label} {...row} />
          ))}
        </div>
      </div>
    </div>
  );
}
