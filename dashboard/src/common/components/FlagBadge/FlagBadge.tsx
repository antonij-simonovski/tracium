export type SilentFailure = 'truncated' | 'refused' | 'tool_error';

export const SILENT_FAILURE_LABEL: Record<SilentFailure, string> = {
  truncated: 'Truncated',
  refused: 'Refused',
  tool_error: 'Tool error',
};

export function FlagBadge({ flag }: { flag: string }) {
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        padding: '1px 7px',
        borderRadius: 999,
        fontSize: 11.5,
        fontWeight: 500,
        whiteSpace: 'nowrap',
        color: 'var(--warning)',
        background: 'color-mix(in srgb, var(--warning) 12%, transparent)',
        border: '1px solid color-mix(in srgb, var(--warning) 32%, transparent)',
      }}
    >
      {SILENT_FAILURE_LABEL[flag as SilentFailure] ?? flag}
    </span>
  );
}
