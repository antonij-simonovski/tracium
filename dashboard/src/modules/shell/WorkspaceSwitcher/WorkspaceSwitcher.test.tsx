import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { WorkspaceSwitcher } from './WorkspaceSwitcher';
import type { Workspace } from '../interfaces';

const ACME: Workspace = { id: 'ws-1' as Workspace['id'], name: 'Acme', slug: 'acme', role: 'owner', members: 1, env: 'production' };
const BETA: Workspace = { id: 'ws-2' as Workspace['id'], name: 'Beta', slug: 'beta', role: 'member', members: 4, env: 'staging' };

function show() {
  const setWorkspace = vi.fn();
  render(
    <WorkspaceSwitcher
      workspace={ACME}
      workspaces={[ACME, BETA]}
      setWorkspace={setWorkspace}
      createWorkspace={vi.fn()}
      deleteWorkspace={vi.fn()}
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: /Acme/, expanded: false }));
  return { setWorkspace };
}

describe('WorkspaceSwitcher', () => {
  it('lists workspaces as buttons and focuses the active one', () => {
    show();
    expect(screen.getByRole('dialog', { name: 'Workspaces' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Acme.*1 member$/ })).toHaveFocus();
    expect(screen.getByText('member · 4 members')).toBeInTheDocument();
  });

  it('switches workspace from the keyboard and returns focus to the trigger', () => {
    const { setWorkspace } = show();
    fireEvent.click(screen.getByRole('button', { name: /Beta/ }));
    expect(setWorkspace).toHaveBeenCalledWith(BETA);
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(screen.getByRole('button', { name: /Acme/, expanded: false })).toHaveFocus();
  });

  it('closes on Escape', () => {
    show();
    fireEvent.keyDown(screen.getByRole('button', { name: /Beta/ }), { key: 'Escape' });
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});
