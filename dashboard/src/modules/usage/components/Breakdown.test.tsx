import { render, screen, fireEvent } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { UserId } from '../../../common/ids';
import type { UserSummary } from '../interfaces';
import { Breakdown, TabPill } from './Breakdown';

const user = (over: Partial<UserSummary>): UserSummary => ({
  id: 'acme' as UserId,
  name: 'Acme',
  cost: 2,
  costPrev: 1,
  runs: 10,
  runsPrev: 20,
  avg: 0.2,
  trend: [],
  ...over,
});

function show(rows: UserSummary[], onRowClick = vi.fn()) {
  render(
    <Breakdown
      rows={rows}
      totalCost={rows.reduce((s, r) => s + r.cost, 0)}
      kind="user"
      sortBy="cost"
      setSortBy={vi.fn()}
      onRowClick={onRowClick}
    />,
  );
  return onRowClick;
}

describe('Breakdown', () => {
  it('flags rising cost and falling runs as bad', () => {
    show([user({})]);
    expect(screen.getByText('100%').className).toMatch(/bad/);
    expect(screen.getByText('50%').className).toMatch(/bad/);
    expect(screen.getByText('300%').className).toMatch(/bad/);
  });

  it('flags falling cost and rising runs as good', () => {
    show([user({ cost: 1, costPrev: 2, runs: 20, runsPrev: 10, avg: 0.05 })]);
    expect(screen.getByText('50%').className).toMatch(/good/);
    expect(screen.getByText('100%').className).toMatch(/good/);
    expect(screen.getByText('75%').className).toMatch(/good/);
  });

  it('shows no delta without a previous period', () => {
    show([user({ costPrev: 0, runsPrev: 0 })]);
    expect(screen.queryByText(/^\d+%$/)).toBeNull();
  });

  it('marks no row as top spend when nothing was spent', () => {
    show([user({ cost: 0 })]);
    expect(screen.queryByTitle('Top 75% of spend')).toBeNull();
  });

  it('leaves the unattributed row unclickable', () => {
    const onRowClick = show([user({}), user({ id: '—' as UserId, name: 'default' })]);
    const rows = screen.getAllByRole('button', { name: /Acme/ });
    expect(rows).toHaveLength(1);
    fireEvent.keyDown(rows[0], { key: 'Enter' });
    expect(onRowClick).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('button', { name: /default/ })).toBeNull();
  });
});

describe('TabPill attribute picker', () => {
  it('exposes its state and keyboard-reachable options', () => {
    const onSelect = vi.fn();
    render(
      <TabPill
        tab="user"
        setTab={vi.fn()}
        tabs={[{ id: 'user', label: 'By client', count: 1 }]}
        attribute={{ keys: ['team', 'env'], value: '', onSelect }}
      />,
    );
    const trigger = screen.getByRole('button', { name: 'By attribute' });
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(trigger);
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    fireEvent.click(screen.getByRole('button', { name: 'env' }));
    expect(onSelect).toHaveBeenCalledWith('env');
  });
});
