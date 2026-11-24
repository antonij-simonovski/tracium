import { useMemo, useState } from 'react';
import type React from 'react';
import { SlicedButton, Spinner, StatusPill, formatDateTime } from '../../../common';
import { ReviewFieldList } from '../../trace-inspector';
import { applyEdits, fieldText, flattenFields, parseFieldText, parseJsonObject } from '../../trace-inspector/utils/reviewFields';
import { useReview } from '../hooks/useReview';
import type { QueueReason } from '../../../common/interfaces';

const label: React.CSSProperties = {
  fontSize: 12, fontWeight: 500, textTransform: 'uppercase', letterSpacing: '0.08em', color: 'var(--muted)', marginBottom: 8,
};
const input: React.CSSProperties = {
  width: '100%', boxSizing: 'border-box', padding: '7px 9px', borderRadius: 7,
  border: '1px solid var(--border)', background: 'var(--surface-alt)', color: 'var(--foreground)',
  fontFamily: 'var(--font-mono)', fontSize: 13,
};

function Button({ children, primary, onClick, disabled }: { children: React.ReactNode; primary?: boolean; onClick: () => void; disabled?: boolean }) {
  if (primary) {
    return <SlicedButton type="button" onClick={onClick} disabled={disabled} style={{ padding: '7px 20px 7px 12px', fontSize: 13.5 }}>{children}</SlicedButton>;
  }
  return (
    <button type="button" onClick={onClick} disabled={disabled} style={{
      fontSize: 13.5, fontWeight: 500, padding: '7px 12px', borderRadius: 8, cursor: disabled ? 'default' : 'pointer',
      fontFamily: 'inherit', opacity: disabled ? 0.5 : 1,
      background: 'transparent', border: '1px solid var(--border)', color: 'var(--foreground)',
    }}>{children}</button>
  );
}

const PILL = {
  correct: { status: 'ok', label: 'Correct' },
  incorrect: { status: 'failed', label: 'Incorrect' },
  missing: { status: 'warning', label: 'Missing' },
} as const;

export interface ReviewPanelProps {
  traceId: string;
  /** Reviews this span instead of the whole run. */
  spanId?: string;
  output: string | null;
  /** Sent with every save when the run was opened from the review queue. */
  reason?: QueueReason;
  onSaved?: () => void;
}

/** Mark a run or span correct, or correct its output; JSON outputs are corrected field by field. */
export function ReviewPanel({ traceId, spanId, output: traceOutput, reason, onSaved }: ReviewPanelProps) {
  const { review, isLoading, error, save, remove } = useReview(traceId, spanId, onSaved);
  const output = review?.output ?? traceOutput ?? '';
  // An API review may have sent its own output; saves must keep grading that one.
  const uploaded = review != null && review.output !== (traceOutput ?? '');
  const kept = { ...(uploaded ? { output } : {}), ...(reason ? { reason } : {}) };
  const obj = useMemo(() => parseJsonObject(output), [output]);
  const fields = useMemo(() => (obj ? flattenFields(obj) : []), [obj]);

  const [editing, setEditing] = useState(false);
  const [raw, setRaw] = useState(false);
  const [edits, setEdits] = useState<Record<string, string>>({});
  const [text, setText] = useState('');
  const [note, setNote] = useState('');

  if (isLoading) return <Spinner size={24} />;
  if (error) return <div style={{ color: 'var(--error)' }}>Failed to load review.</div>;

  const startEditing = () => {
    const prior = review?.corrected_output;
    const base = (prior && parseJsonObject(prior)) || obj;
    setEdits(base && obj ? Object.fromEntries(flattenFields(base).filter(f => fields.some(o => o.path === f.path)).map(f => [f.path, fieldText(f.value)])) : {});
    // The output's own text, not a re-serialized parse, so big numbers keep every digit.
    setText(prior ?? output);
    const paths = (o: Record<string, unknown>) => flattenFields(o).map(f => f.path).join('\n');
    setRaw(!obj || (base !== obj && paths(base!) !== paths(obj)));
    setNote(review?.note ?? '');
    setEditing(true);
  };

  const submit = () => {
    const done = { onSuccess: () => setEditing(false) };
    if (raw || !obj) return save.mutate({ ...kept, corrected_output: text, note }, done);
    // Only changed fields go up, so untouched ones keep the output's exact JSON (big numbers survive).
    const changed = fields.filter(f => f.path in edits && edits[f.path] !== fieldText(f.value));
    // The API splits paths on dots, so a key with a dot in it needs the whole object.
    // shortcut: that object is re-serialized from JSON.parse, so integers past 2^53 lose digits; send the raw text if that matters.
    if (changed.some(f => f.keys.some(k => k.includes('.')))) {
      return save.mutate({ ...kept, corrected_output: JSON.stringify(applyEdits(obj, fields, edits), null, 2), note }, done);
    }
    save.mutate({ ...kept, corrections: Object.fromEntries(changed.map(f => [f.path, parseFieldText(edits[f.path], f.value)])), note }, done);
  };

  const busy = save.isPending || remove.isPending;

  return (
    <div style={{ maxWidth: 820, display: 'grid', gap: 20 }}>
      {review && !editing && (
        <div style={{ display: 'grid', gap: 14 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
            <StatusPill status={PILL[review.verdict].status}>{PILL[review.verdict].label}</StatusPill>
            <span style={{ fontSize: 13, color: 'var(--muted)' }}>
              Reviewed {formatDateTime(Date.parse(review.updated_at))}{review.source === 'api' && ' · sent by API'}
            </span>
          </div>
          {uploaded && <div style={{ fontSize: 13, color: 'var(--muted)' }}>Graded against an output sent by API, not the traced output.</div>}
          {review.note && <div style={{ fontSize: 14, color: 'var(--foreground)' }}>{review.note}</div>}
          {review.fields.length > 0 && <ReviewFieldList fields={review.fields} />}
          {review.fields.length === 0 && review.corrected_output !== null && (
            <div>
              <div style={label}>Corrected output</div>
              <pre style={{ ...input, whiteSpace: 'pre-wrap', margin: 0 }}>{review.corrected_output}</pre>
            </div>
          )}
        </div>
      )}

      {!review && !editing && !output && (
        <div style={{ fontSize: 14, color: 'var(--muted)' }}>No output was recorded for this {spanId ? 'span' : 'run'}, so it can only be marked correct, incorrect or missing.</div>
      )}

      {editing && (
        <div style={{ display: 'grid', gap: 14 }}>
          {obj && (
            <label style={{ display: 'flex', gap: 8, alignItems: 'center', fontSize: 13.5, color: 'var(--muted)' }}>
              <input type="checkbox" checked={raw} onChange={e => setRaw(e.target.checked)} />
              Edit as raw JSON (to add or remove fields)
            </label>
          )}
          {!raw && obj ? (
            <div role="table" aria-label="Correct fields" style={{ display: 'grid', gap: 8 }}>
              {fields.map(f => {
                const changed = f.path in edits && edits[f.path] !== fieldText(f.value);
                return (
                  <div role="row" key={f.path} style={{ display: 'grid', gridTemplateColumns: 'minmax(120px, 1fr) 2fr', gap: 12, alignItems: 'center' }}>
                    <span role="cell" style={{ fontFamily: 'var(--font-mono)', fontSize: 13, color: changed ? 'var(--error)' : 'var(--muted)', wordBreak: 'break-word' }}>{f.path}</span>
                    <input
                      aria-label={f.path}
                      value={edits[f.path] ?? fieldText(f.value)}
                      onChange={e => setEdits(prev => ({ ...prev, [f.path]: e.target.value }))}
                      style={{ ...input, borderColor: changed ? 'var(--error)' : 'var(--border)' }}
                    />
                  </div>
                );
              })}
            </div>
          ) : (
            <textarea aria-label="Corrected output" value={text} onChange={e => setText(e.target.value)} rows={12} style={{ ...input, resize: 'vertical' }} />
          )}
          <input aria-label="Note" placeholder="Note (optional)" value={note} onChange={e => setNote(e.target.value)} style={{ ...input, fontFamily: 'inherit' }} />
          <div style={{ display: 'flex', gap: 8 }}>
            <Button primary onClick={submit} disabled={busy}>Save correction</Button>
            <Button onClick={() => setEditing(false)} disabled={busy}>Cancel</Button>
          </div>
        </div>
      )}

      {!editing && (
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          <Button primary onClick={() => save.mutate({ ...kept, verdict: 'correct' })} disabled={busy}>Mark correct</Button>
          {output
            ? <Button onClick={startEditing} disabled={busy}>{review?.corrected_output != null ? 'Edit correction' : 'Correct output'}</Button>
            : <Button onClick={() => save.mutate({ ...kept, verdict: 'incorrect' })} disabled={busy}>Mark incorrect</Button>}
          <Button onClick={() => save.mutate({ ...kept, verdict: 'missing' })} disabled={busy}>Mark missing</Button>
          {review && <Button onClick={() => remove.mutate()} disabled={busy}>Remove review</Button>}
        </div>
      )}
      {(save.error || remove.error) && <div style={{ color: 'var(--error)', fontSize: 13.5 }}>Could not save the review. Try again.</div>}
    </div>
  );
}
