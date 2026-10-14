import { useEffect, useRef, useState } from 'react';
import { IconChevron, fmtNum } from '../../../common';
import hover from '../../../common/styles/hover.module.css';
import type { WorkflowVersion } from '../interfaces';
import { VersionTag } from './VersionTag';

interface VersionPickerProps {
  label: string;
  value: string;
  versions: WorkflowVersion[];
  exclude: string;
  onChange: (version: string) => void;
}

export function VersionPicker({ label, value, versions, exclude, onChange }: VersionPickerProps) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false);
    document.addEventListener('mousedown', close);
    document.addEventListener('keydown', esc);
    return () => {
      document.removeEventListener('mousedown', close);
      document.removeEventListener('keydown', esc);
    };
  }, [open]);

  const options = versions.filter((v) => v.version && v.version !== exclude);
  const pick = (version: string) => {
    setOpen(false);
    if (version !== value) onChange(version);
  };

  return (
    <div ref={ref} style={{ position: 'relative', minWidth: 0 }}>
      <button
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={`${label}: ${value}`}
        className={hover.outlined}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          maxWidth: '100%',
          padding: '3px 8px',
          margin: '-4px -9px',
          background: 'transparent',
          border: '1px solid transparent',
          borderRadius: 7,
        }}
      >
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: 15, fontWeight: 500, color: 'var(--foreground)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {value}
        </span>
        <IconChevron size={12} />
      </button>
      {open && (
        <div
          role="listbox"
          aria-label={label}
          style={{
            position: 'absolute',
            top: 'calc(100% + 10px)',
            left: -9,
            width: 280,
            maxHeight: 360,
            overflowY: 'auto',
            zIndex: 60,
            background: 'var(--surface)',
            border: '1px solid var(--border-strong)',
            borderRadius: 10,
            boxShadow: '0 12px 40px rgba(0,0,0,0.5)',
            paddingBottom: 6,
          }}
        >
          <div style={{ padding: '8px 10px 6px', fontSize: 11, fontWeight: 600, color: 'var(--muted)', textTransform: 'uppercase', letterSpacing: '0.1em' }}>
            {label}
          </div>
          {options.length === 0 && <div style={{ padding: '8px 10px', fontSize: 13, color: 'var(--muted)' }}>No other versions in this range</div>}
          {options.map((o) => (
            <button
              key={o.version}
              role="option"
              aria-selected={o.version === value}
              onClick={() => pick(o.version)}
              className={hover.row}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 10,
                width: '100%',
                padding: '8px 10px',
                border: 'none',
                textAlign: 'left',
                background: o.version === value ? 'var(--surface-active)' : undefined,
              }}
            >
              <span style={{ flex: 1, minWidth: 0, display: 'flex', alignItems: 'center', gap: 8 }}>
                <span style={{ fontFamily: 'var(--font-mono)', fontSize: 13, color: 'var(--foreground)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {o.version}
                </span>
                <VersionTag source={o.source} />
              </span>
              <span style={{ fontSize: 12, color: 'var(--muted)', fontVariantNumeric: 'tabular-nums' }}>{fmtNum(o.calls)} runs</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
