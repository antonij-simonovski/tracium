import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { ReleaseLines } from '../../overview/components/ReleaseLines';
import type { ReleaseReport } from '../interfaces';
import { ReleaseFindings } from './ReleaseFindings';
import { VersionsTable } from './VersionsTable';

const now = Date.parse('2026-11-10T12:00:00Z');

const report = (version: string, over: Partial<ReleaseReport> = {}): ReleaseReport => ({
  workspace_id: 'ws', workflow: 'checkout-agent', version, prev_version: 'v1', first_seen_at: '2026-11-10T08:00:00Z',
  status: 'final', verdict: 'regressed', runs: 38, prev_runs: 412, created_at: '2026-11-10T08:05:00Z', finalized_at: '2026-11-10T09:00:00Z',
  findings: [
    { metric: 'truncation_rate', prev: 0.008, next: 0.12, verdict: 'regressed', severity: 'warning' },
    { metric: 'cost_per_run', prev: 0.02, next: 0.04, verdict: 'regressed', severity: 'critical' },
    { metric: 'latency_p95', prev: 2000, next: 2100, verdict: 'same', severity: '' },
  ],
  ...over,
});

describe('release reports', () => {
  it('shows each finding with both values and the change', () => {
    render(<ReleaseFindings report={report('v2')} />);
    expect(screen.getByText('Regressed')).toBeTruthy();
    expect(screen.getByText('Truncated LLM calls')).toBeTruthy();
    expect(screen.getByText('12.0%')).toBeTruthy();
    expect(screen.getByText('+11.2 pts')).toBeTruthy();
    expect(screen.getAllByText('same')).toHaveLength(1);
  });

  it('badges each version by its verdict', () => {
    const v = (version: string, report_verdict?: 'pending' | 'ok' | 'insufficient') => ({
      version, first_seen_ms: now, last_seen_ms: now, calls: 40, cost: 1, cost_per_run: 0.02, p95_latency_ms: 100, error_rate: 0, tokens_per_run: 10, report_verdict,
    });
    render(<VersionsTable versions={[v('v3', 'pending'), v('v2', 'ok'), v('v1', 'insufficient'), v('v0')]} rangeLabel="7 days" onCompare={() => {}} />);
    expect(screen.getByText('Checking')).toBeTruthy();
    expect(screen.getByText('Healthy')).toBeTruthy();
    expect(screen.getByText('Not enough runs')).toBeTruthy();
  });

  it('lists the last day of releases on the overview', () => {
    render(<ReleaseLines now={now} reports={[report('v42'), report('v9', { created_at: '2026-11-08T08:00:00Z' })]} />);
    expect(screen.getByText(/of checkout-agent: regressed, 2 findings/)).toBeTruthy();
    expect(screen.queryByText('v9')).toBeNull();
  });
});
