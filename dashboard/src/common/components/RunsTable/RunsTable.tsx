import { StatusPill } from '../StatusPill';
import { fmtCost, fmtMs } from '../../utils/formatters';
import hover from '../../styles/hover.module.css';

export interface RunRow {
  id: string;
  /** Shown above the id when the rows span several workflows. */
  name?: string;
  status: 'completed' | 'failed';
  /** Human-relative start time, e.g. "2m ago". */
  time: string;
  duration: number;
  cost: number;
  err?: string | null;
  version?: string;
}

export interface RunsTableProps {
  runs: RunRow[];
  onOpen: (id: string) => void;
  emptyText?: string;
  /** Adds a Version column after the trace id. */
  showVersion?: boolean;
}

// [Trace, Version?, Status, Started, Duration, Cost]
const GRID = '1.4fr 110px 1fr 90px 80px';
const GRID_VERSION = '1.4fr 120px 110px 1fr 90px 80px';
const MIN_WIDTH = 560;
const MIN_WIDTH_VERSION = 680;

const ellipsis = { whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' } as const;
const numeric = { fontSize: 13.5, textAlign: 'right', color: 'var(--foreground)', fontVariantNumeric: 'tabular-nums' } as const;

export function RunsTable({ runs, onOpen, emptyText = 'No runs in this window.', showVersion = false }: RunsTableProps) {
  const grid = showVersion ? GRID_VERSION : GRID;
  const minWidth = showVersion ? MIN_WIDTH_VERSION : MIN_WIDTH;
  return (
    <div style={{ overflowX: 'auto' }}>
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: grid,
          minWidth,
          gap: 12,
          padding: '0 6px 10px',
          borderBottom: '1px solid var(--border)',
          fontSize: 12,
          color: 'var(--muted)',
          fontWeight: 500,
          textTransform: 'uppercase',
          letterSpacing: '0.06em',
        }}
      >
        <span>Trace</span>
        {showVersion && <span>Version</span>}
        <span>Status</span>
        <span>Started</span>
        <span style={{ textAlign: 'right' }}>Duration</span>
        <span style={{ textAlign: 'right' }}>Cost</span>
      </div>
      {runs.length === 0 ? (
        <div style={{ padding: '32px 6px', fontSize: 14, color: 'var(--muted)' }}>{emptyText}</div>
      ) : (
        runs.map((run, i) => (
          <button
            key={run.id}
            onClick={() => onOpen(run.id)}
            className={hover.row}
            style={{
              display: 'grid',
              gridTemplateColumns: grid,
              gap: 12,
              alignItems: 'center',
              width: '100%',
              minWidth,
              font: 'inherit',
              color: 'inherit',
              padding: '12px 6px',
              border: 'none',
              textAlign: 'left',
              borderBottom: i < runs.length - 1 ? '1px solid color-mix(in srgb, var(--border) 50%, transparent)' : 'none',
            }}
          >
            <span style={{ display: 'flex', flexDirection: 'column', gap: 3, minWidth: 0 }}>
              {run.name && (
                <span style={{ fontSize: 14, fontWeight: 500, color: 'var(--foreground)', ...ellipsis }}>{run.name}</span>
              )}
              <span
                style={{
                  fontFamily: 'var(--font-mono)',
                  fontSize: run.name ? 11 : 12.5,
                  color: run.name ? 'var(--muted)' : 'var(--foreground)',
                  ...ellipsis,
                }}
              >
                {run.id}
              </span>
              {run.err && (
                <span style={{ fontSize: 12, fontFamily: 'var(--font-mono)', color: 'var(--error)', ...ellipsis }}>
                  {run.err}
                </span>
              )}
            </span>
            {showVersion && (
              <span style={{ fontFamily: 'var(--font-mono)', fontSize: 12.5, color: 'var(--muted)', ...ellipsis }}>
                {run.version || '—'}
              </span>
            )}
            <StatusPill status={run.status} />
            <span style={{ fontSize: 13.5, color: 'var(--muted)' }}>{run.time}</span>
            <span style={numeric}>{fmtMs(run.duration)}</span>
            <span style={numeric}>{fmtCost(run.cost)}</span>
          </button>
        ))
      )}
    </div>
  );
}
