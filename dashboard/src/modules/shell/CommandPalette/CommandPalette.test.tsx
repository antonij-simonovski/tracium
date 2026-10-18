import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { CommandPalette } from './CommandPalette';

function show() {
  const onClose = vi.fn();
  const onSelect = vi.fn();
  render(<CommandPalette open onClose={onClose} onSelect={onSelect} />);
  return { onClose, onSelect };
}

describe('CommandPalette', () => {
  it('opens as a modal dialog with the search focused', () => {
    show();
    expect(screen.getByRole('dialog', { name: 'Command palette' })).toHaveAttribute('aria-modal', 'true');
    expect(screen.getByRole('textbox', { name: 'Search views' })).toHaveFocus();
  });

  it('selects the first match after the query changes', () => {
    const { onSelect, onClose } = show();
    const input = screen.getByRole('textbox', { name: 'Search views' });
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    fireEvent.change(input, { target: { value: 'sett' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onSelect).toHaveBeenCalledWith({ view: 'settings' });
    expect(onClose).toHaveBeenCalled();
  });

  it('closes on Escape from a result, not only from the search', () => {
    const { onClose } = show();
    const usage = screen.getByRole('button', { name: 'Usage' });
    usage.focus();
    fireEvent.keyDown(usage, { key: 'Escape' });
    expect(onClose).toHaveBeenCalled();
  });

  it('keeps Tab inside the dialog', () => {
    show();
    const settings = screen.getByRole('button', { name: 'Settings' });
    settings.focus();
    fireEvent.keyDown(settings, { key: 'Tab' });
    expect(screen.getByRole('textbox', { name: 'Search views' })).toHaveFocus();
  });
});
