interface TrafficBucket {
  label: string;
  base: number;
  candidate: number;
  total: number;
}

// Per-bucket share of the workflow's runs: base, candidate, and every other version.
export function TrafficStrip({ buckets, baseLabel, candidateLabel }: { buckets: TrafficBucket[]; baseLabel: string; candidateLabel: string }) {
  const gap = buckets.length > 40 ? 1 : 3;
  const pct = (n: number, total: number) => `${Math.round((n / (total || 1)) * 100)}%`;
  return (
    <div style={{ display: 'flex', gap, marginLeft: 56, marginRight: 56 }}>
      {buckets.map((b, i) => {
        const other = Math.max(0, b.total - b.base - b.candidate);
        return (
          <div
            key={i}
            title={`${b.label}: ${baseLabel} ${pct(b.base, b.total)} · ${candidateLabel} ${pct(b.candidate, b.total)} · other ${pct(other, b.total)}`}
            style={{ flex: 1, height: 14, borderRadius: gap > 1 ? 3 : 1, overflow: 'hidden', display: 'flex', background: b.total ? undefined : 'var(--surface-alt)' }}
          >
            {other > 0 && <div style={{ flex: other, background: 'var(--surface-active)' }} />}
            {b.base > 0 && <div style={{ flex: b.base, background: 'color-mix(in srgb, var(--foreground) 32%, transparent)' }} />}
            {b.candidate > 0 && <div style={{ flex: b.candidate, background: 'var(--accent)', opacity: 0.85 }} />}
          </div>
        );
      })}
    </div>
  );
}
