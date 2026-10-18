import { useEffect, useState } from 'react';
import { relativeTime } from '../../utils/time';

export type SyncTone = 'live' | 'stale' | 'error' | 'syncing';

const TONE_COLOR: Record<SyncTone, string> = {
  live: 'var(--accent)',
  stale: 'var(--warning)',
  error: 'var(--error)',
  syncing: 'var(--muted)',
};

export interface LastUpdatedProps {
  // Epoch ms of the last fetch; the label then reads "Updated …" and ticks.
  at?: number;
  label?: string;
  tone?: SyncTone;
}

export function LastUpdated({ at, label, tone = 'live' }: LastUpdatedProps) {
  const ticking = label == null && at != null;
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!ticking) return;
    const id = setInterval(() => setNow(Date.now()), 10_000);
    return () => clearInterval(id);
  }, [ticking]);

  const text = label ?? (at != null ? `Updated ${relativeTime(at, now)}` : '');
  const dotColor = TONE_COLOR[tone];
  const textColor = tone === 'live' || tone === 'syncing' ? 'var(--muted)' : dotColor;

  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 7,
        fontSize: 13,
        color: textColor,
        flexShrink: 0,
        whiteSpace: 'nowrap',
      }}
    >
      <span
        style={{
          width: 7,
          height: 7,
          borderRadius: '50%',
          background: dotColor,
          boxShadow: tone === 'live' ? `0 0 0 3px color-mix(in srgb, ${dotColor} 18%, transparent)` : 'none',
        }}
      />
      {text}
    </div>
  );
}
