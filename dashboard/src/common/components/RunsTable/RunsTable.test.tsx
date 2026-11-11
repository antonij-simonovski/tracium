import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { RunsTable } from './RunsTable';

describe('RunsTable', () => {
  it('shows a badge per silent-failure flag', () => {
    render(<RunsTable onOpen={() => {}} runs={[{ id: 't1', status: 'completed', time: 'now', duration: 1, cost: 0, flags: ['refused', 'tool_error', 'truncated'] }]} />);
    for (const label of ['Truncated', 'Refused', 'Tool error']) expect(screen.getByText(label)).toBeTruthy();
  });
});
