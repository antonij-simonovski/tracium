import { SEVERITY_META, type Severity } from '../utils/severity';

const LABEL: Record<Severity, string> = { critical: 'Critical', warning: 'Warning', info: 'Info' };

// The text label keeps severity readable without colour.
export function SeverityTag({ severity, compact = false }: { severity: Severity; compact?: boolean }) {
  const { color } = SEVERITY_META[severity];
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5, fontSize: compact ? 11.5 : 12.5, fontWeight: 500, color, whiteSpace: 'nowrap' }}>
      <span aria-hidden style={{ width: 8, height: 8, borderRadius: '50%', background: color, flex: 'none' }} />
      {LABEL[severity]}
    </span>
  );
}
