import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { SetupChecks } from './SetupChecks';
import type { SetupCheck } from '../interfaces';

const unmetered: SetupCheck = {
  code: 'unmetered',
  severity: 'warning',
  message: 'Streamed calls arrived without token usage.',
  spans: 3,
  share: 0.25,
  example_trace_id: 't1',
};
const rejected: SetupCheck = { ...unmetered, code: 'rejected_invalid_timestamp', severity: 'critical', message: 'Spans were rejected.', example_trace_id: '' };

const renderChecks = (checks: SetupCheck[], onOpenTrace = vi.fn()) =>
  render(<SetupChecks checks={checks} workspaceId="ws-1" onOpenTrace={onOpenTrace} />);

describe('SetupChecks', () => {
  beforeEach(() => localStorage.clear());

  it('renders nothing when every check passes', () => {
    const { container } = renderChecks([]);
    expect(container.innerHTML).toBe('');
  });

  it('shows a collapsed banner that expands into the issues', () => {
    const onOpenTrace = vi.fn();
    renderChecks([rejected, unmetered], onOpenTrace);

    expect(screen.getByText('2 potential setup issues in the last 24 hours')).toBeTruthy();
    expect(screen.queryByText(unmetered.message)).toBeNull();

    fireEvent.click(screen.getByText('Review'));
    expect(screen.getByText(unmetered.message)).toBeTruthy();
    expect(screen.getAllByText('View example trace')).toHaveLength(1);
    fireEvent.click(screen.getByText('View example trace'));
    expect(onOpenTrace).toHaveBeenCalledWith('t1');
  });

  it('dismisses every issue from the banner', () => {
    const { container } = renderChecks([rejected, unmetered]);
    fireEvent.click(screen.getByText('Dismiss'));
    expect(container.innerHTML).toBe('');
  });

  it('remembers issues muted with "Don\'t show again"', () => {
    const { unmount } = renderChecks([rejected, unmetered]);
    fireEvent.click(screen.getByText('Review'));
    fireEvent.click(within(screen.getByText(unmetered.message)).getByText("Don't show again"));
    expect(screen.getByText('1 potential setup issue in the last 24 hours')).toBeTruthy();
    unmount();

    renderChecks([rejected, unmetered]);
    fireEvent.click(screen.getByText('Review'));
    expect(screen.queryByText(unmetered.message)).toBeNull();
    expect(screen.getByText(rejected.message)).toBeTruthy();
  });

  it('ignores corrupt muted-check storage', () => {
    localStorage.setItem('tracium_muted_checks_ws-1', '{not json');
    renderChecks([unmetered]);
    expect(screen.getByText('1 potential setup issue in the last 24 hours')).toBeTruthy();
  });
});
