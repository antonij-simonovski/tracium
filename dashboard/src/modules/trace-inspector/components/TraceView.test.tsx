import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { TraceView } from './TraceView';
import type { SpanDetail, TraceDetail } from '../interfaces';
import type { SpanId, TraceId } from '../../../common/ids';

function span(id: string, depth: number, extra: Partial<SpanDetail> = {}): SpanDetail {
  return {
    id: id as SpanId, name: id, type: 'internal', start: 0, duration: 10, depth,
    cost: 0, tokens: 0, status: 'ok', attributes: {}, ...extra,
  };
}

const failure = { type: 'Timeout', message: 'tool timed out', code: '', stack: '' };

const trace: TraceDetail = {
  id: 't1' as TraceId,
  workflow: 'checkout',
  status: 'failed',
  duration: 100,
  totalCost: 0,
  inputTokens: 0,
  outputTokens: 0,
  model: '',
  output: null,
  error: failure,
  spans: [
    span('root', 0, { childCount: 2 }),
    span('plan', 1, { childCount: 1 }),
    span('lookup', 2, { status: 'failed', error: failure }),
    span('reply', 1, { setupIssues: [{ code: 'unpriced_model', severity: 'warning', message: 'No price for this model.' }] }),
  ],
};

const renderTrace = () => render(<TraceView trace={trace} setView={vi.fn()} />);
const row = (name: string) =>
  screen.queryAllByText(name).map(el => el.closest('button[aria-pressed]')).find(Boolean) ?? null;

describe('TraceView', () => {
  beforeEach(() => {
    vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })));
  });

  it('shows the client beside the trace times and opens it', () => {
    const onOpenUser = vi.fn();
    render(<TraceView trace={{ ...trace, user: 'acme', startedAt: '10:00:00' }} setView={vi.fn()} onOpenUser={onOpenUser} />);
    const chip = screen.getByTitle('Open client');
    expect(chip).toHaveTextContent('acme');
    expect(chip.parentElement).toHaveTextContent('10:00:00');
    fireEvent.click(chip);
    expect(onOpenUser).toHaveBeenCalledWith('acme');
  });

  it('selects the first failing span initially', () => {
    renderTrace();
    expect(row('lookup')).toHaveAttribute('aria-pressed', 'true');
  });

  it('shows the selected span\'s setup issues', () => {
    renderTrace();
    expect(screen.queryByText('No price for this model.')).toBeNull();
    fireEvent.click(row('reply')!);
    expect(screen.getByText('Setup issues')).toBeInTheDocument();
    expect(screen.getByText('No price for this model.')).toBeInTheDocument();
  });

  it('collapses a parent and reveals the failing span again', () => {
    renderTrace();
    const toggle = screen.getByRole('button', { name: 'Collapse root' });
    expect(toggle).toHaveAttribute('aria-expanded', 'true');

    fireEvent.click(toggle);
    expect(screen.getByRole('button', { name: 'Expand root' })).toHaveAttribute('aria-expanded', 'false');
    expect(row('plan')).toBeNull();
    expect(row('lookup')).toBeNull();

    fireEvent.click(screen.getByText('Jump to failing span →'));
    expect(row('plan')).toBeInTheDocument();
    expect(row('lookup')).toHaveAttribute('aria-pressed', 'true');
  });
});
