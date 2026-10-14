import { useCallback, useRef, useState } from 'react';
import type { CSSProperties, ReactNode } from 'react';
import { IconChevron, fmtNum, useClickOutside } from '../../../common';
import hover from '../../../common/styles/hover.module.css';

export interface VersionOption {
  id: string | null;
  label: string;
  calls: number;
}

interface VersionDropdownProps {
  label: string;
  value: string | null;
  options: VersionOption[];
  onChange: (id: string | null) => void;
  trigger: ReactNode;
  triggerLabel?: string;
  triggerStyle: CSSProperties;
  popoverStyle: CSSProperties;
  emptyText?: string;
}

export function VersionDropdown({ label, value, options, onChange, trigger, triggerLabel, triggerStyle, popoverStyle, emptyText }: VersionDropdownProps) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const close = useCallback(() => setOpen(false), []);
  useClickOutside(ref, open, close);

  const pick = (id: string | null) => {
    setOpen(false);
    if (id !== value) onChange(id);
  };

  return (
    <div ref={ref} style={{ position: 'relative', flexShrink: 0 }} onKeyDown={(e) => e.key === 'Escape' && setOpen(false)}>
      <button
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={triggerLabel}
        className={hover.outlined}
        style={{ display: 'flex', alignItems: 'center', borderRadius: 7, ...triggerStyle }}
      >
        {trigger}
        <IconChevron size={12} />
      </button>
      {open && (
        <div
          role="listbox"
          aria-label={label}
          style={{
            position: 'absolute',
            width: 'max-content',
            minWidth: 280,
            maxHeight: 360,
            overflowY: 'auto',
            zIndex: 60,
            background: 'var(--surface)',
            border: '1px solid var(--border-strong)',
            borderRadius: 10,
            boxShadow: '0 12px 40px rgba(0,0,0,0.5)',
            paddingBottom: 6,
            ...popoverStyle,
          }}
        >
          <div style={{ padding: '8px 10px 6px', fontSize: 11, fontWeight: 600, color: 'var(--muted)', textTransform: 'uppercase', letterSpacing: '0.1em' }}>
            {label}
          </div>
          {options.length === 0 && emptyText && <div style={{ padding: '8px 10px', fontSize: 13, color: 'var(--muted)' }}>{emptyText}</div>}
          {options.map((o) => (
            <button
              key={o.id ?? ''}
              role="option"
              aria-selected={o.id === value}
              onClick={() => pick(o.id)}
              className={hover.row}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 10,
                width: '100%',
                padding: '8px 10px',
                border: 'none',
                textAlign: 'left',
                background: o.id === value ? 'var(--surface-active)' : undefined,
              }}
            >
              <span style={{ flex: 1, fontFamily: 'var(--font-mono)', fontSize: 13, color: 'var(--foreground)', whiteSpace: 'nowrap' }}>
                {o.label}
              </span>
              <span style={{ fontSize: 12, color: 'var(--muted)', fontVariantNumeric: 'tabular-nums', whiteSpace: 'nowrap', flexShrink: 0 }}>{fmtNum(o.calls)} runs</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
