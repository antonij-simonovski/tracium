import React from 'react';
import { IconDot } from '../icons';

export interface StatusPillProps {
  status: string;
  children?: React.ReactNode;
}

const STATUS_MAP: Record<string, { color: string; label: string }> = {
  completed: { color: 'var(--accent)',   label: 'Completed' },
  failed:    { color: 'var(--error)',    label: 'Failed'    },
  warning:   { color: 'var(--warning)',  label: 'Warning'   },
  healthy:   { color: 'var(--accent)',   label: 'Healthy'   },
  critical:  { color: 'var(--error)',    label: 'Critical'  },
  ok:        { color: 'var(--accent)',   label: 'Ok'        },
};

export function StatusPill({ status, children }: StatusPillProps) {
  const s = STATUS_MAP[status] ?? { color: 'var(--muted)', label: status };

  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        padding: '3px 9px 3px 7px',
        borderRadius: 999,
        fontSize: 13,
        fontWeight: 500,
        color: s.color,
        background: `color-mix(in srgb, ${s.color} 10%, transparent)`,
        border: `1px solid color-mix(in srgb, ${s.color} 22%, transparent)`,
      }}
    >
      <IconDot size={6} color={s.color} />
      {children ?? s.label}
    </span>
  );
}
