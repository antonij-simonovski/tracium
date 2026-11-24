import { useState } from 'react';
import { Section, plural, relativeTime } from '../../../common';
import hover from '../../../common/styles/hover.module.css';
import { IncidentRow } from '../components/IncidentRow';
import { useIncidents } from '../hooks/useIncidents';
import type { Incident, IncidentStatus } from '../interfaces';
import { openIncident, openSubject } from '../utils/navigation';

interface IncidentsPageProps {
  setView: (v: string) => void;
  setSelected: (updater: (prev: Record<string, string>) => Record<string, string>) => void;
}

const TABS: { id: IncidentStatus; label: string }[] = [
  { id: 'open', label: 'Open' },
  { id: 'resolved', label: 'Resolved' },
];

export function IncidentsPage({ setView, setSelected }: IncidentsPageProps) {
  const [tab, setTab] = useState<IncidentStatus>('open');
  const list = useIncidents(tab);
  const items = list.data?.items ?? [];
  const total = list.data?.total ?? 0;
  const lastChecked = list.data?.last_evaluated_at;

  const open = (id: string) => openIncident(id, setView, setSelected);
  const onSubject = (inc: Incident) => openSubject(inc, setView, setSelected);

  return (
    <div style={{ padding: 'clamp(20px, 4vw, 32px) clamp(16px, 4vw, 40px) 64px', maxWidth: 1100, margin: '0 auto' }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginBottom: 22 }}>
        <h1 style={{ fontSize: 26, fontWeight: 600, letterSpacing: '-0.02em', margin: 0 }}>Incidents</h1>
        <p style={{ fontSize: 14, color: 'var(--muted)', margin: 0 }}>
          What Tracium's background checks found wrong, grouped by workflow, client or workspace.
        </p>
      </div>

      <div role="tablist" aria-label="Incident status" style={{ display: 'flex', gap: 4, borderBottom: '1px solid var(--border)', marginBottom: 8 }}>
        {TABS.map((t) => {
          const active = tab === t.id;
          return (
            <button
              key={t.id}
              role="tab"
              aria-selected={active}
              onClick={() => setTab(t.id)}
              style={{
                padding: '8px 12px',
                background: 'none',
                border: 'none',
                borderBottom: `2px solid ${active ? 'var(--accent)' : 'transparent'}`,
                marginBottom: -1,
                font: 'inherit',
                fontSize: 14,
                fontWeight: active ? 600 : 400,
                color: active ? 'var(--foreground)' : 'var(--muted)',
                cursor: 'pointer',
              }}
            >
              {t.label}
              {active && list.data ? ` · ${total}` : ''}
            </button>
          );
        })}
      </div>

      <Section isLoading={list.isLoading} isError={list.isError && !list.data}>
        {items.length === 0 ? (
          tab === 'open' ? (
            <div style={{ padding: '48px 16px', textAlign: 'center', display: 'flex', flexDirection: 'column', gap: 8, alignItems: 'center' }}>
              <p style={{ fontSize: 17, fontWeight: 500, margin: 0 }}>Nothing looks wrong right now</p>
              <p style={{ fontSize: 14, color: 'var(--muted)', margin: 0 }}>
                {lastChecked ? `Last checked ${relativeTime(Date.parse(lastChecked))}.` : 'Not checked yet.'} Tracium checks every workspace for anomalies in the background.
              </p>
              <button onClick={() => { setSelected((s) => ({ ...s, settingsTab: 'alerts' })); setView('settings'); }} className={hover.text} style={{ background: 'none', border: 'none', font: 'inherit', fontSize: 14, cursor: 'pointer', textDecoration: 'underline' }}>
                Alert settings
              </button>
            </div>
          ) : (
            <p style={{ padding: '48px 16px', textAlign: 'center', fontSize: 14, color: 'var(--muted)', margin: 0 }}>No resolved incidents yet.</p>
          )
        ) : (
          <>
            {items.map((inc) => (
              <IncidentRow key={inc.id} incident={inc} onOpen={open} onOpenSubject={onSubject} />
            ))}
            {total > items.length && (
              <p style={{ fontSize: 13, color: 'var(--muted)', margin: '12px 6px 0' }}>
                Showing the latest {plural(items.length, 'incident')} of {total}.
              </p>
            )}
          </>
        )}
      </Section>
    </div>
  );
}
