import type { Anomaly } from '../../interfaces';
import { anomalyChips } from '../../utils/anomalies';

interface OutlierChipsProps {
  anomalies: Anomaly[];
}

export function OutlierChips({ anomalies }: OutlierChipsProps) {
  const chips = anomalyChips(anomalies);
  if (chips.length === 0) return null;
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap', marginBottom: 28 }}>
      <span style={{ fontSize: 13, color: 'var(--muted)' }}>Outliers this window:</span>
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        {chips.map((c) => (
          <span
            key={c.metric}
            style={{
              fontSize: 12,
              padding: '5px 10px',
              borderRadius: 20,
              background: c.tint,
              color: c.color,
              border: `1px solid ${c.border}`,
              whiteSpace: 'nowrap',
            }}
          >
            {c.label}
          </span>
        ))}
      </div>
    </div>
  );
}
