import { render, screen, fireEvent } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { UsersPage, type User } from './UsersPage';

const row = (id: string, cost: number, runs: number): User => ({ id, name: id, cost, runs, avg: cost / runs, trend: [] });

function show(users: User[], comparison?: { runs: number; cost: number }) {
  const setView = vi.fn();
  const setSelected = vi.fn();
  render(<UsersPage users={users} periodLabel="Last 7 days" comparison={comparison} setView={setView} setSelected={setSelected} />);
  return { setView, setSelected };
}

beforeEach(() => {
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })));
});

afterEach(() => vi.unstubAllGlobals());

describe('UsersPage', () => {
  it('opens a client from the keyboard but not the unattributed row', () => {
    const { setView, setSelected } = show([row('acme', 2, 10), row('—', 1, 5)]);
    const rows = screen.getAllByRole('button', { name: /acme/ });
    expect(rows).toHaveLength(1);
    fireEvent.keyDown(rows[0], { key: 'Enter' });
    expect(setView).toHaveBeenCalledWith('user');
    expect(setSelected).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('button', { name: /—/ })).toBeNull();
  });

  it('marks falling runs as a bad change', () => {
    show([row('acme', 2, 10)], { runs: 20, cost: 2 });
    const runs = screen.getByText('50%');
    expect(runs.style.color).toBe('var(--error)');
    expect(screen.getByText('vs prev period')).toBeInTheDocument();
  });
});
