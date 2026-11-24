import { useCallback, useRef, useState } from 'react';
import { IconChevron, useClickOutside } from '../../../common';
import hover from '../../../common/styles/hover.module.css';
import type { MuteDuration, MuteInput } from '../interfaces';

const OPTIONS: { id: MuteDuration; label: string }[] = [
  { id: '24h', label: 'For 24 hours' },
  { id: '7d', label: 'For 7 days' },
  { id: 'forever', label: 'Forever' },
];

interface MuteMenuProps {
  metric: string;
  metricLabel: string;
  disabled?: boolean;
  onMute: (input: MuteInput) => void;
}

export function MuteMenu({ metric, metricLabel, disabled, onMute }: MuteMenuProps) {
  const [open, setOpen] = useState(false);
  const [onlyMetric, setOnlyMetric] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const close = useCallback(() => setOpen(false), []);
  useClickOutside(ref, open, close);

  return (
    <div ref={ref} style={{ position: 'relative' }} onKeyDown={(e) => e.key === 'Escape' && setOpen(false)}>
      <button
        onClick={() => setOpen((o) => !o)}
        disabled={disabled}
        aria-haspopup="menu"
        aria-expanded={open}
        className={hover.outlined}
        style={{ display: 'inline-flex', alignItems: 'center', gap: 6, padding: '8px 14px', borderRadius: 8, border: '1px solid', background: 'transparent', font: 'inherit', fontSize: 13.5, cursor: 'pointer' }}
      >
        Mute <IconChevron size={12} />
      </button>
      {open && (
        <div
          role="menu"
          style={{ position: 'absolute', right: 0, top: 'calc(100% + 6px)', zIndex: 20, minWidth: 220, padding: 6, background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: 10, boxShadow: '0 8px 24px var(--shadow-popover)' }}
        >
          {OPTIONS.map((o) => (
            <button
              key={o.id}
              role="menuitem"
              onClick={() => {
                setOpen(false);
                onMute({ duration: o.id, metric: onlyMetric ? metric : '' });
              }}
              className={hover.row}
              style={{ display: 'block', width: '100%', padding: '8px 10px', border: 'none', borderRadius: 6, textAlign: 'left', font: 'inherit', fontSize: 13.5, color: 'var(--foreground)' }}
            >
              {o.label}
            </button>
          ))}
          <label style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '8px 10px 4px', marginTop: 4, borderTop: '1px solid var(--border)', fontSize: 13, color: 'var(--muted)', cursor: 'pointer' }}>
            <input type="checkbox" checked={onlyMetric} onChange={(e) => setOnlyMetric(e.target.checked)} />
            Only {metricLabel.toLowerCase()}
          </label>
        </div>
      )}
    </div>
  );
}
