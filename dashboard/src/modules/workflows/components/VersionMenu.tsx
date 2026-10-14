import { useEffect, useRef, useState } from 'react';
import { IconChevron, fmtNum } from '../../../common';
import hover from '../../../common/styles/hover.module.css';
import type { WorkflowVersion } from '../interfaces';
import { VersionTag } from './VersionTag';

interface VersionMenuProps {
  versions: WorkflowVersion[];
  value: string | null;
  onChange: (version: string | null) => void;
}

export function VersionMenu({ versions, value, onChange }: VersionMenuProps) {
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

  const total = versions.reduce((s, v) => s + v.calls, 0);
  const options = [
    { id: null, label: 'All versions', calls: total, source: '' as const },
    ...versions.filter((v) => v.version).map((v) => ({ id: v.version, label: v.version, calls: v.calls, source: v.source })),
  ];
  const pick = (id: string | null) => {
    onChange(id);
    setOpen(false);
  };

  return (
    <div ref={ref} style={{ position: 'relative' }}>
      <button
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="listbox"
        aria-expanded={open}
        className={hover.outlined}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          padding: '6px 10px',
          background: 'var(--surface-alt)',
          border: '1px solid var(--border)',
          borderRadius: 7,
          fontSize: 13,
        }}
      >
        <span>Version</span>
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: 12.5, color: 'var(--foreground)' }}>{value ?? 'All versions'}</span>
        <IconChevron size={12} />
      </button>
      {open && (
        <div
          role="listbox"
          aria-label="Filter by version"
          style={{
            position: 'absolute',
            top: 'calc(100% + 6px)',
            right: 0,
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
            Filter by version
          </div>
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
              <span style={{ flex: 1, minWidth: 0, display: 'flex', alignItems: 'center', gap: 8 }}>
                <span style={{ fontFamily: 'var(--font-mono)', fontSize: 13, color: 'var(--foreground)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {o.label}
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
