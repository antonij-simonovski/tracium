import type { ReleaseReport } from '../../workflows/interfaces';
import { VerdictBadge } from '../../workflows/components/VerdictBadge';

const DAY_MS = 24 * 60 * 60 * 1000;

// One line per release reported in the last 24 hours.
export function ReleaseLines({ reports, now = Date.now() }: { reports: ReleaseReport[]; now?: number }) {
  const recent = reports.filter((r) => now - Date.parse(r.created_at) < DAY_MS);
  if (recent.length === 0) return null;
  return (
    <div style={{ marginBottom: 28, display: 'flex', flexDirection: 'column', gap: 6 }}>
      {recent.map((r) => {
        const notable = r.findings.filter((f) => f.verdict !== 'same').length;
        const verdict = r.status === 'pending' ? 'checking' : r.verdict === 'insufficient' ? 'not enough runs' : r.verdict;
        return (
          <div key={`${r.workspace_id}/${r.workflow}/${r.version}`} style={{ display: 'flex', alignItems: 'center', gap: 10, fontSize: 13.5, color: 'var(--foreground)', flexWrap: 'wrap' }}>
            <VerdictBadge verdict={r.status === 'pending' ? 'pending' : r.verdict} />
            <span>
              <span style={{ fontFamily: 'var(--font-mono)' }}>{r.version}</span> of {r.workflow}: {verdict}
              {notable > 0 && `, ${notable} finding${notable === 1 ? '' : 's'}`}
            </span>
          </div>
        );
      })}
    </div>
  );
}
