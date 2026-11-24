import type { ReviewField } from '../../../common/interfaces';

const mono = { fontFamily: 'var(--font-mono)', fontSize: 13, wordBreak: 'break-word' } as const;

/** Graded JSON fields, wrong ones first, each showing the model's value against the corrected one. */
export function ReviewFieldList({ fields }: { fields: ReviewField[] }) {
  const sorted = [...fields].sort((a, b) => Number(a.correct) - Number(b.correct));
  return (
    <div role="table" aria-label="Field results" style={{ border: '1px solid var(--border)', borderRadius: 8 }}>
      {sorted.map((f, i) => (
        <div role="row" key={f.path} style={{
          display: 'grid', gridTemplateColumns: 'minmax(120px, 1fr) 2fr', gap: 12, padding: '9px 12px',
          borderTop: i ? '1px solid var(--border)' : 'none',
          background: f.correct ? 'transparent' : 'color-mix(in srgb, var(--error) 6%, transparent)',
        }}>
          <span role="cell" style={{ ...mono, color: f.correct ? 'var(--muted)' : 'var(--error)' }}>{f.path}</span>
          <span role="cell" style={mono}>
            {f.correct
              ? <span style={{ color: 'var(--muted)' }}>{f.output}</span>
              : <>
                  {f.missing
                    ? <span style={{ color: 'var(--warning)' }}>missing</span>
                    : <del style={{ color: 'var(--muted)' }}>{f.output}</del>}
                  <span style={{ color: 'var(--muted)', margin: '0 6px' }}>→</span>
                  <span style={{ color: 'var(--foreground)' }}>{f.corrected || 'removed'}</span>
                </>}
          </span>
        </div>
      ))}
    </div>
  );
}
