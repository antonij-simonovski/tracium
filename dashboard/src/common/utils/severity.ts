export type Severity = 'info' | 'warning' | 'critical';

// Severity → colour tokens. color-mix keeps the tint/border derived from one
// token, matching how StatusPill build their surfaces (no hardcoded rgba).
export const SEVERITY_META: Record<Severity, { color: string; tint: string; border: string; rank: number }> = {
  critical: { color: 'var(--error)', tint: 'color-mix(in srgb, var(--error) 14%, transparent)', border: 'color-mix(in srgb, var(--error) 42%, transparent)', rank: 3 },
  warning: { color: 'var(--warning)', tint: 'color-mix(in srgb, var(--warning) 14%, transparent)', border: 'color-mix(in srgb, var(--warning) 42%, transparent)', rank: 2 },
  // info uses a neutral tone, not the brand accent — a flag is never "good",
  // and a green one would read that way.
  info: { color: 'var(--muted-foreground)', tint: 'color-mix(in srgb, var(--muted-foreground) 16%, transparent)', border: 'color-mix(in srgb, var(--muted-foreground) 42%, transparent)', rank: 1 },
};
