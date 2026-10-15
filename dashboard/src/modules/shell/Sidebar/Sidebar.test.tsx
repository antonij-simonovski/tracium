import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { Sidebar } from './Sidebar';

const props = {
  currentView: 'overview',
  setView: () => {},
  workspace: null,
  workspaces: [],
  setWorkspace: () => {},
  createWorkspace: () => {},
  deleteWorkspace: async () => {},
};

afterEach(() => localStorage.clear());

describe('Sidebar collapse', () => {
  it('hides labels when collapsed and remembers the choice', () => {
    const { unmount } = render(<Sidebar {...props} />);
    expect(screen.getByText('Overview')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Collapse sidebar' }));
    expect(screen.queryByText('Overview')).toBeNull();
    expect(screen.getByRole('button', { name: 'Overview' })).toBeTruthy();
    unmount();

    render(<Sidebar {...props} />);
    expect(screen.getByRole('button', { name: 'Expand sidebar' })).toBeTruthy();
  });

  it('ignores the collapsed preference in the mobile drawer', () => {
    localStorage.setItem('tracium_sidebar', 'collapsed');
    render(<Sidebar {...props} mobile open />);
    expect(screen.getByText('Overview')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Expand sidebar' })).toBeNull();
  });
});

describe('Sidebar theme toggle', () => {
  afterEach(() => delete document.documentElement.dataset.theme);

  it('switches to light and remembers it', () => {
    document.documentElement.dataset.theme = 'dark';
    render(<Sidebar {...props} />);
    fireEvent.click(screen.getByRole('button', { name: 'Light mode' }));
    expect(document.documentElement.dataset.theme).toBe('light');
    expect(localStorage.getItem('tracium_theme')).toBe('light');
    expect(screen.getByRole('button', { name: 'Dark mode' })).toBeTruthy();
  });

  it('is hidden and saves nothing when embedded', () => {
    render(<Sidebar {...props} embedded />);
    expect(screen.queryByRole('button', { name: 'Light mode' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Collapse sidebar' }));
    expect(localStorage.getItem('tracium_sidebar')).toBeNull();
  });
});

describe('Sidebar star link', () => {
  it('links to the GitHub repo', () => {
    render(<Sidebar {...props} />);
    expect(screen.getByRole('link', { name: 'Star on GitHub' }).getAttribute('href')).toBe('https://github.com/antonij-tracium/tracium');
  });
});
