import { bucketLabel } from '../../../common';
import type { VersionChange } from '../../overview/interfaces';
import { useWorkflowVersions } from '../../workflows/hooks/useWorkflowDetail';
import { previousVersion } from '../../workflows/utils';

interface LikelyCauseProps {
  change: VersionChange;
  firstSignalMs: number;
  onCompare: (workflow: string, base: string, candidate: string) => void;
  onOpenWorkflow: (workflow: string) => void;
}

function gapLabel(ms: number): string {
  const h = Math.max(0, Math.round(ms / 3_600_000));
  return h < 48 ? `${h}h` : `${Math.round(h / 24)}d`;
}

export function LikelyCause({ change, firstSignalMs, onCompare, onOpenWorkflow }: LikelyCauseProps) {
  const versions = useWorkflowVersions(change.workflow, '30d').data?.items ?? [];
  const current = versions.find((v) => v.version === change.version);
  const prev = current && previousVersion(versions, current);
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '8px 20px', padding: '14px 16px', borderRadius: 10, border: '1px solid var(--accent-border)', background: 'var(--accent-soft)' }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 3, flex: '1 1 240px' }}>
        <span style={{ fontSize: 12, letterSpacing: '0.1em', textTransform: 'uppercase', color: 'var(--muted)' }}>Likely cause</span>
        <span style={{ fontSize: 15, fontWeight: 600 }}>
          {change.workflow} <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--accent)' }}>{change.version}</span>
        </span>
        <span style={{ fontSize: 13, color: 'var(--muted)' }}>
          First seen {bucketLabel(change.first_seen_ms, '7d')}
          {change.first_seen_ms % 86_400_000 !== 0 && ` · first signal ${gapLabel(firstSignalMs - change.first_seen_ms)} later`}
        </span>
      </div>
      <button
        onClick={() => (prev ? onCompare(change.workflow, prev.version, change.version) : onOpenWorkflow(change.workflow))}
        style={{ fontSize: 13, padding: '8px 14px', borderRadius: 8, border: '1px solid var(--accent-border)', background: 'transparent', color: 'var(--accent)', cursor: 'pointer' }}
      >
        {prev ? `Compare with ${prev.version} →` : `Open ${change.workflow} →`}
      </button>
    </div>
  );
}
