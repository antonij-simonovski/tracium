import { useState } from 'react';
import { fmtNum, fmtPct, SetupIssueNote, SEVERITY_META } from '../../../common';
import type { SetupCheck } from '../interfaces';

interface SetupChecksProps {
  checks: SetupCheck[];
  workspaceId: string;
  onOpenTrace: (traceId: string) => void;
}

const linkStyle = {
  padding: 0,
  border: 'none',
  background: 'transparent',
  color: 'var(--foreground)',
  fontFamily: 'inherit',
  fontSize: 'inherit',
  textDecoration: 'underline',
  cursor: 'pointer',
} as const;

const mutedKey = (workspaceId: string) => `tracium_muted_checks_${workspaceId}`;

function readMuted(workspaceId: string): Set<string> {
  try {
    const codes: unknown = JSON.parse(localStorage.getItem(mutedKey(workspaceId)) ?? '[]');
    return new Set(Array.isArray(codes) ? codes.filter((c): c is string => typeof c === 'string') : []);
  } catch {
    return new Set();
  }
}

export function SetupChecks({ checks, workspaceId, onOpenTrace }: SetupChecksProps) {
  const [open, setOpen] = useState(false);
  const [dismissed, setDismissed] = useState<Set<string>>(new Set());
  const [muted, setMuted] = useState(() => readMuted(workspaceId));

  const visible = checks.filter((c) => !dismissed.has(c.code) && !muted.has(c.code));
  if (visible.length === 0) return null;

  const dismiss = (...codes: string[]) => setDismissed((s) => new Set([...s, ...codes]));
  const mute = (code: string) => {
    const next = new Set(muted).add(code);
    try {
      localStorage.setItem(mutedKey(workspaceId), JSON.stringify([...next]));
    } catch {
      // Storage unavailable: the mute still applies for this session.
    }
    setMuted(next);
  };
  // Checks arrive most severe first.
  const color = SEVERITY_META[visible[0].severity].color;

  return (
    <div style={{ marginBottom: 28, border: '1px solid var(--border)', borderRadius: 10, background: 'var(--surface)' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap', padding: '10px 14px', fontSize: 13.5 }}>
        <span style={{ width: 8, height: 8, borderRadius: '50%', background: color, flexShrink: 0 }} />
        <span style={{ flex: 1, color: 'var(--foreground)' }}>
          {visible.length} potential setup issue{visible.length === 1 ? '' : 's'} in the last 24 hours
        </span>
        <button style={linkStyle} aria-expanded={open} onClick={() => setOpen((o) => !o)}>{open ? 'Hide' : 'Review'}</button>
        <button style={linkStyle} onClick={() => dismiss(...visible.map((c) => c.code))}>Dismiss</button>
      </div>
      {open && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8, padding: '0 14px 14px' }}>
          {visible.map((c) => (
            <SetupIssueNote key={c.code} issue={c}>
              <span style={{ fontVariantNumeric: 'tabular-nums' }}>
                {fmtNum(c.spans)} span{c.spans === 1 ? '' : 's'} · {fmtPct(c.share * 100)}
              </span>
              {c.example_trace_id && (
                <button style={linkStyle} onClick={() => onOpenTrace(c.example_trace_id)}>View example trace</button>
              )}
              <button style={linkStyle} onClick={() => dismiss(c.code)}>Dismiss</button>
              <button style={linkStyle} onClick={() => mute(c.code)}>Don't show again</button>
            </SetupIssueNote>
          ))}
        </div>
      )}
    </div>
  );
}
