import { SILENT_FAILURE_LABEL, type SilentFailure } from '../../../common';

export type SilentFailureValue = SilentFailure | 'any';

const OPTIONS: { id: SilentFailureValue | undefined; label: string }[] = [
  { id: undefined, label: 'All runs' },
  { id: 'any', label: 'Any flag' },
  ...(Object.keys(SILENT_FAILURE_LABEL) as SilentFailure[]).map((id) => ({ id, label: SILENT_FAILURE_LABEL[id] })),
];

export function SilentFailureFilter({ value, onChange }: { value?: SilentFailureValue; onChange: (v: SilentFailureValue | undefined) => void }) {
  return (
    <div role="group" aria-label="Silent failures" style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 4 }}>
      <span style={{ fontSize: 12.5, color: 'var(--muted)', marginRight: 4 }}>Silent failures</span>
      {OPTIONS.map((o) => {
        const active = value === o.id;
        return (
          <button
            key={o.label}
            aria-pressed={active}
            onClick={() => onChange(o.id)}
            style={{
              padding: '3px 10px',
              borderRadius: 999,
              font: 'inherit',
              fontSize: 12.5,
              cursor: 'pointer',
              border: `1px solid ${active ? 'color-mix(in srgb, var(--warning) 40%, transparent)' : 'var(--border)'}`,
              background: active ? 'color-mix(in srgb, var(--warning) 12%, transparent)' : 'transparent',
              color: active ? 'var(--warning)' : 'var(--muted)',
            }}
          >
            {o.label}
          </button>
        );
      })}
    </div>
  );
}
