import { StatusPill } from '../../../common/components/StatusPill';
import type { ReleaseVerdict } from '../interfaces';

const VERDICTS: Record<ReleaseVerdict, { status: string; label: string }> = {
  ok: { status: 'healthy', label: 'Healthy' },
  improved: { status: 'healthy', label: 'Improved' },
  regressed: { status: 'critical', label: 'Regressed' },
  pending: { status: 'pending', label: 'Checking' },
  insufficient: { status: 'insufficient', label: 'Not enough runs' },
};

export function VerdictBadge({ verdict }: { verdict?: ReleaseVerdict | '' }) {
  if (!verdict) return null;
  const v = VERDICTS[verdict];
  return <StatusPill status={v.status}>{v.label}</StatusPill>;
}
