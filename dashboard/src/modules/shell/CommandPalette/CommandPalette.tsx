import React, { useState, useMemo } from 'react';
import {
  IconHome,
  IconWorkflows,
  IconUsers,
  IconUsage,
  IconKey,
  IconSettings,
  IconSearch,
  IconChevronRight,
  handleDialogKeyDown,
} from '../../../common';
import type { CommandAction } from '../interfaces';

interface CmdItemData {
  label: string;
  icon: React.ReactNode;
  action: CommandAction;
}

interface CommandPaletteProps {
  open: boolean;
  onClose: () => void;
  onSelect: (action: CommandAction | undefined) => void;
}

const CMD_ITEMS: CmdItemData[] = [
  { label: 'Overview', icon: <IconHome size={14} />, action: { view: 'overview' } },
  { label: 'Workflows', icon: <IconWorkflows size={14} />, action: { view: 'workflows' } },
  { label: 'Usage', icon: <IconUsage size={14} />, action: { view: 'usage' } },
  { label: 'Clients', icon: <IconUsers size={14} />, action: { view: 'users' } },
  { label: 'API Keys', icon: <IconKey size={14} />, action: { view: 'keys' } },
  { label: 'Settings', icon: <IconSettings size={14} />, action: { view: 'settings' } },
];

export function CommandPalette({ open, ...props }: CommandPaletteProps): React.ReactElement | null {
  return open ? <Palette {...props} /> : null;
}

function Palette({ onClose, onSelect }: Omit<CommandPaletteProps, 'open'>): React.ReactElement {
  const [query, setQuery] = useState('');
  const [idx, setIdx] = useState(0);

  const filtered = useMemo<CmdItemData[]>(() => {
    if (!query.trim()) return CMD_ITEMS;
    const q = query.toLowerCase();
    return CMD_ITEMS.filter((item) => item.label.toLowerCase().includes(q));
  }, [query]);

  const choose = (action: CommandAction | undefined) => {
    onSelect(action);
    onClose();
  };

  const handleKey = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setIdx((i) => Math.min(i + 1, filtered.length - 1));
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault();
      setIdx((i) => Math.max(i - 1, 0));
    }
    if (e.key === 'Enter') {
      e.preventDefault();
      choose(filtered[idx]?.action);
    }
  };

  return (
    <div
      onClick={onClose}
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(4,8,7,0.7)',
        backdropFilter: 'blur(6px)',
        zIndex: 200,
        display: 'grid',
        placeItems: 'center start',
        paddingTop: '15vh',
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Command palette"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => handleDialogKeyDown(e, onClose)}
        style={{
          width: 560,
          maxWidth: '92vw',
          margin: '0 auto',
          background: 'var(--surface)',
          border: '1px solid var(--border-strong)',
          borderRadius: 12,
          boxShadow: '0 24px 60px var(--shadow-popover)',
          overflow: 'hidden',
        }}
      >
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 10,
            padding: '10px 14px',
            borderBottom: '1px solid var(--border)',
          }}
        >
          <IconSearch size={15} style={{ color: 'var(--muted)', flexShrink: 0 }} />
          <input
            autoFocus
            aria-label="Search views"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setIdx(0);
            }}
            onKeyDown={handleKey}
            placeholder="Search views…"
            style={{
              flex: 1,
              background: 'transparent',
              border: 'none',
              outline: 'none',
              fontSize: 15,
              color: 'var(--foreground)',
              fontFamily: 'inherit',
            }}
          />
          <kbd
            style={{
              fontSize: 11,
              padding: '2px 6px',
              borderRadius: 4,
              background: 'var(--surface-alt)',
              border: '1px solid var(--border)',
              color: 'var(--muted)',
              fontFamily: 'inherit',
            }}
          >
            Esc
          </kbd>
        </div>
        <div style={{ maxHeight: 340, overflowY: 'auto' }}>
          {filtered.length === 0 ? (
            <div
              style={{
                padding: '32px 20px',
                textAlign: 'center',
                color: 'var(--muted)',
                fontSize: 14,
              }}
            >
              No results
            </div>
          ) : (
            filtered.map((item, i) => (
              <button
                key={item.label}
                onClick={() => choose(item.action)}
                onMouseEnter={() => setIdx(i)}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 12,
                  width: '100%',
                  padding: '10px 14px',
                  background: i === idx ? 'var(--surface-active)' : 'transparent',
                  border: 'none',
                  cursor: 'pointer',
                  textAlign: 'left',
                  color: i === idx ? 'var(--foreground)' : 'var(--muted)',
                  fontSize: 14,
                }}
              >
                <span style={{ flexShrink: 0, color: 'var(--muted)' }}>{item.icon}</span>
                {item.label}
                {i === idx && (
                  <IconChevronRight
                    size={12}
                    style={{ marginLeft: 'auto', color: 'var(--muted)' }}
                  />
                )}
              </button>
            ))
          )}
        </div>
        <div
          style={{
            padding: '8px 14px',
            borderTop: '1px solid var(--border)',
            display: 'flex',
            gap: 14,
            fontSize: 12,
            color: 'var(--muted)',
          }}
        >
          <span>↑↓ navigate</span>
          <span>↵ select</span>
          <span>Esc close</span>
        </div>
      </div>
    </div>
  );
}
