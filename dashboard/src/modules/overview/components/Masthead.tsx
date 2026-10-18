import { LastUpdated, type SyncTone } from '../../../common';

export interface SyncStatus {
  label: string;
  tone: SyncTone;
}

interface MastheadProps {
  eyebrow?: string;
  title: string;
  subtitle: string;
  status?: SyncStatus;
}

export function Masthead({
  eyebrow,
  title,
  subtitle,
  status = { label: 'Synced just now', tone: 'live' },
}: MastheadProps) {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'flex-end',
        justifyContent: 'space-between',
        gap: 16,
        flexWrap: 'wrap',
        marginBottom: 26,
      }}
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        {eyebrow && (
          <span
            style={{
              fontSize: 12,
              fontWeight: 500,
              textTransform: 'uppercase',
              letterSpacing: '0.1em',
              color: 'var(--muted)',
            }}
          >
            {eyebrow}
          </span>
        )}
        <h1
          style={{
            fontSize: 26,
            fontWeight: 600,
            letterSpacing: '-0.02em',
            margin: 0,
            color: 'var(--foreground)',
          }}
        >
          {title}
        </h1>
        <p style={{ fontSize: 14, color: 'var(--muted)', margin: 0 }}>{subtitle}</p>
      </div>
      <LastUpdated label={status.label} tone={status.tone} />
    </div>
  );
}
