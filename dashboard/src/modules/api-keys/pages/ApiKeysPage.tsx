import React, { useState, useMemo } from 'react';
import {
  IconSearch,
  IconCopy,
  IconTrash,
  IconKey,
  IconCheck,
  IconAlert,
  StatusPill,
  Spinner,
  SlicedButton,
  SectionHead,
  formatDate,
  relativeTime,
  useCopy,
} from '../../../common';
import hover from '../../../common/styles/hover.module.css';
import { useApiKeys, useDemoApiKeys } from '../hooks/useApiKeys';
import type { UseApiKeysResult } from '../hooks/useApiKeys';
import type { ApiKeyRecord, CreatedApiKey } from '../../../common/api';

export interface ApiKeysPageProps {
  /** Seed the demo keys (auth-page preview). A real workspace starts with none. */
  demo?: boolean;
  /** The workspace to manage keys for, in the live (non-demo) page. */
  workspaceId?: string;
}

function fmtLastUsed(iso: string | null): string {
  if (!iso) return 'Never';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  return relativeTime(d.getTime());
}

function FieldLabel({ children }: { children: React.ReactNode }) {
  return (
    <div
      style={{
        fontSize: 12,
        color: 'var(--muted)',
        textTransform: 'uppercase',
        letterSpacing: '0.08em',
        fontWeight: 500,
        marginBottom: 8,
      }}
    >
      {children}
    </div>
  );
}

interface InlineCreateFormProps {
  onCreate: (name: string) => void;
  submitting: boolean;
  error: string | null;
}

function InlineCreateForm({ onCreate, submitting, error }: InlineCreateFormProps) {
  const [name, setName] = useState('');

  const canCreate = name.trim().length > 0 && !submitting;

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        if (canCreate) onCreate(name.trim());
      }}
      style={{ padding: '4px 0 18px' }}
    >
      <FieldLabel>New key name</FieldLabel>
      <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
        <input
          aria-label="New key name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="e.g. Production ingest"
          style={{
            flex: '1 1 260px',
            minWidth: 200,
            padding: '9px 12px',
            background: 'transparent',
            border: '1px solid var(--border-strong)',
            borderRadius: 7,
            color: 'var(--foreground)',
            fontSize: 14,
            outline: 'none',
            fontFamily: 'inherit',
            boxSizing: 'border-box',
          }}
        />
        <SlicedButton type="submit" disabled={!canCreate}>
          {submitting ? 'Creating…' : 'Create key'}
        </SlicedButton>
      </div>
      <div style={{ fontSize: 13, color: 'var(--muted)', marginTop: 8 }}>
        A label to recognise this key later. It grants ingest access to this
        workspace. The full token is shown once, right after it's created.
      </div>
      {error && (
        <div
          style={{
            marginTop: 12,
            fontSize: 13.5,
            color: 'var(--error)',
            display: 'flex',
            gap: 8,
            alignItems: 'center',
          }}
        >
          <IconAlert size={14} />
          {error}
        </div>
      )}
    </form>
  );
}

interface InlineRevealProps {
  created: CreatedApiKey;
  onClose: () => void;
}

function InlineReveal({ created, onClose }: InlineRevealProps) {
  const [copyStatus, copy] = useCopy();
  const copied = copyStatus === 'copied';
  const copyFailed = copyStatus === 'failed';
  const token = created.token;

  return (
    <div
      style={{
        padding: '18px 0',
        borderBottom: '1px solid color-mix(in srgb, var(--border) 50%, transparent)',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 12 }}>
        <span style={{ color: 'var(--accent)', flexShrink: 0 }}>
          <IconKey size={15} />
        </span>
        <div
          style={{
            fontSize: 15,
            fontWeight: 600,
            letterSpacing: '-0.015em',
            color: 'var(--foreground)',
          }}
        >
          Key created. Copy it now
        </div>
        <span style={{ fontSize: 13.5, color: 'var(--muted)' }}>
          This is the only time the full token is shown.
        </span>
      </div>

      <FieldLabel>{created.key.name}</FieldLabel>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 10,
          padding: '12px 14px',
          background: 'var(--surface-alt)',
          border: '1px solid var(--border-strong)',
          borderRadius: 8,
          fontFamily: 'var(--font-mono)',
          fontSize: 13.5,
          color: 'var(--foreground)',
          wordBreak: 'break-all',
        }}
      >
        <span style={{ flex: 1 }}>{token}</span>
        <button
          onClick={() => copy(token)}
          style={{
            padding: '5px 11px',
            background: copied ? 'var(--accent)' : 'transparent',
            border:
              '1px solid ' +
              (copied
                ? 'var(--accent)'
                : copyFailed
                  ? 'var(--error)'
                  : 'var(--border-strong)'),
            borderRadius: 6,
            color: copied
              ? 'var(--accent-contrast)'
              : copyFailed
                ? 'var(--error)'
                : 'var(--foreground)',
            fontSize: 12.5,
            fontWeight: 500,
            fontFamily: 'inherit',
            flexShrink: 0,
            cursor: 'pointer',
          }}
        >
          {copied ? 'Copied' : copyFailed ? 'Copy failed' : 'Copy'}
        </button>
      </div>
      {copyFailed && (
        <div style={{ marginTop: 8, fontSize: 13, color: 'var(--error)' }}>
          Couldn't copy automatically. Select the token above and copy it manually.
        </div>
      )}

      <div
        style={{
          marginTop: 12,
          fontSize: 13.5,
          color: 'var(--muted)',
          display: 'flex',
          gap: 10,
          alignItems: 'flex-start',
        }}
      >
        <span style={{ color: 'var(--warning)', flexShrink: 0, marginTop: 1 }}>
          <IconAlert size={14} />
        </span>
        <span>
          Store it in a secrets manager and send it as{' '}
          <code style={{ fontFamily: 'var(--font-mono)', color: 'var(--foreground)' }}>
            Authorization: Bearer &lt;token&gt;
          </code>{' '}
          on OTLP ingest. From now on the dashboard shows only{' '}
          <code style={{ fontFamily: 'var(--font-mono)', color: 'var(--foreground)' }}>
            {created.key.prefix}…
          </code>
        </span>
      </div>

      <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 14 }}>
        <SlicedButton onClick={onClose}>
          Done, I've stored it safely
        </SlicedButton>
      </div>
    </div>
  );
}

interface RevokeConfirmRowProps {
  target: ApiKeyRecord;
  onCancel: () => void;
  onConfirm: (id: string) => void;
  submitting: boolean;
  error: string | null;
}

function RevokeConfirmRow({ target, onCancel, onConfirm, submitting, error }: RevokeConfirmRowProps) {
  return (
    <div
      style={{
        padding: '14px 14px',
        borderLeft: '2px solid var(--error)',
        background: 'color-mix(in srgb, var(--error) 6%, transparent)',
        borderBottom: '1px solid color-mix(in srgb, var(--border) 50%, transparent)',
        display: 'flex',
        flexDirection: 'column',
        gap: 12,
      }}
    >
      <div style={{ fontSize: 13.5, color: 'var(--muted)', lineHeight: 1.55 }}>
        <span style={{ color: 'var(--foreground)', fontWeight: 500 }}>
          Revoke “{target.name}”?
        </span>{' '}
        Ingest requests presenting{' '}
        <code style={{ fontFamily: 'var(--font-mono)' }}>{target.prefix}…</code>{' '}
        will be rejected shortly after (subject to the collector's short
        verification cache). This cannot be undone.
      </div>
      {error && (
        <div
          style={{
            fontSize: 13.5,
            color: 'var(--error)',
            display: 'flex',
            gap: 8,
            alignItems: 'center',
          }}
        >
          <IconAlert size={14} />
          {error}
        </div>
      )}
      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
        <button
          onClick={onCancel}
          disabled={submitting}
          style={{
            padding: '7px 13px',
            background: 'transparent',
            border: '1px solid var(--border-strong)',
            borderRadius: 7,
            color: 'var(--foreground)',
            fontSize: 14,
            fontFamily: 'inherit',
            cursor: submitting ? 'not-allowed' : 'pointer',
          }}
        >
          Cancel
        </button>
        <button
          onClick={() => onConfirm(target.id)}
          disabled={submitting}
          style={{
            padding: '7px 13px',
            background: 'color-mix(in srgb, var(--error) 12%, transparent)',
            border: '1px solid color-mix(in srgb, var(--error) 30%, transparent)',
            borderRadius: 7,
            color: 'var(--error)',
            fontSize: 14,
            fontWeight: 500,
            fontFamily: 'inherit',
            cursor: submitting ? 'not-allowed' : 'pointer',
          }}
        >
          {submitting ? 'Revoking…' : 'Revoke key'}
        </button>
      </div>
    </div>
  );
}

const LIVE_COLS = [
  { label: 'Name',       w: 'minmax(200px,1.6fr)', align: 'left'  as const },
  { label: 'Key',        w: 'minmax(150px,1fr)',   align: 'left'  as const },
  { label: 'Status',     w: '96px',                align: 'left'  as const },
  { label: 'Last used',  w: '120px',               align: 'left'  as const },
  { label: 'Created',    w: '120px',               align: 'left'  as const },
  { label: '',           w: '36px',                align: 'right' as const },
];
const LIVE_TMPL = LIVE_COLS.map((c) => c.w).join(' ');

interface LiveKeyRowProps {
  k: ApiKeyRecord;
  isLast: boolean;
  confirming: boolean;
  onRevoke: (k: ApiKeyRecord) => void;
}

function LiveKeyRow({ k, isLast, confirming, onRevoke }: LiveKeyRowProps) {
  const [copyStatus, copy] = useCopy();
  const copied = copyStatus === 'copied';
  const revoked = !!k.revoked_at;

  return (
    <div
      className={hover.row}
      style={{
        display: 'grid',
        gridTemplateColumns: LIVE_TMPL,
        minWidth: 760,
        gap: 14,
        alignItems: 'center',
        padding: '14px 0',
        borderBottom:
          isLast || confirming
            ? 'none'
            : '1px solid color-mix(in srgb, var(--border) 50%, transparent)',
        opacity: revoked ? 0.6 : 1,
      }}
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 3, minWidth: 0 }}>
        <span
          style={{
            fontSize: 14.5,
            fontWeight: 500,
            color: 'var(--foreground)',
            whiteSpace: 'nowrap',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            textDecoration: revoked ? 'line-through' : 'none',
            textDecorationColor: 'var(--muted)',
          }}
        >
          {k.name}
        </span>
        {revoked && k.revoked_at && (
          <div style={{ fontSize: 12.5, color: 'var(--muted)' }}>
            Revoked {formatDate(k.revoked_at)}
          </div>
        )}
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: 6, minWidth: 0 }}>
        <code
          style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 13,
            color: 'var(--muted)',
            whiteSpace: 'nowrap',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            flex: 1,
          }}
        >
          {k.prefix}…
        </code>
        <button
          onClick={() => copy(k.prefix)}
          title="Copy prefix"
          aria-label={`Copy prefix of ${k.name}`}
          className={hover.text}
          style={{
            padding: '4px 6px',
            background: 'transparent',
            border: 'none',
            color: copied ? 'var(--accent)' : undefined,
            flexShrink: 0,
            transition: 'color .12s',
            fontFamily: 'inherit',
            cursor: 'pointer',
          }}
        >
          {copied ? <IconCheck size={12} /> : <IconCopy size={12} />}
        </button>
      </div>

      <div style={{ minWidth: 0 }}>
        <StatusPill status={revoked ? 'failed' : 'ok'}>
          {revoked ? 'Revoked' : 'Active'}
        </StatusPill>
      </div>

      <div style={{ fontSize: 13.5, color: 'var(--foreground)' }}>
        {fmtLastUsed(k.last_used_at)}
      </div>

      <div style={{ fontSize: 13.5, color: 'var(--muted)', fontVariantNumeric: 'tabular-nums' }}>
        {formatDate(k.created_at)}
      </div>

      <div style={{ textAlign: 'right' }}>
        {!revoked && !confirming && (
          <button
            onClick={() => onRevoke(k)}
            title="Revoke"
            aria-label={`Revoke ${k.name}`}
            className={hover.danger}
            style={{
              padding: '5px 6px',
              background: 'transparent',
              border: 'none',
              transition: 'color .12s',
              fontFamily: 'inherit',
              cursor: 'pointer',
            }}
          >
            <IconTrash size={13} />
          </button>
        )}
      </div>
    </div>
  );
}

// ApiKeysView is the presentational page: it owns the create/reveal/revoke UI
// state and renders whatever key data it is handed. The data source is injected
// so the live page (real backend) and the demo (in-memory mock) share one UI.
function ApiKeysView({ data, canCreate }: { data: UseApiKeysResult; canCreate: boolean }) {
  const {
    keys,
    isLoading,
    isError,
    createKey,
    isCreating,
    revokeKey,
    isRevoking,
  } = data;

  const [search, setSearch] = useState('');
  const [createError, setCreateError] = useState<string | null>(null);
  const [created, setCreated] = useState<CreatedApiKey | null>(null);
  const [revokeTarget, setRevokeTarget] = useState<ApiKeyRecord | null>(null);
  const [revokeError, setRevokeError] = useState<string | null>(null);

  const active = useMemo(() => keys.filter((k) => !k.revoked_at), [keys]);
  const revoked = useMemo(() => keys.filter((k) => !!k.revoked_at), [keys]);

  const visible = useMemo(() => {
    if (!search.trim()) return keys;
    const s = search.toLowerCase();
    return keys.filter(
      (k) => k.name.toLowerCase().includes(s) || k.prefix.toLowerCase().includes(s),
    );
  }, [keys, search]);

  async function handleCreate(name: string) {
    setCreateError(null);
    try {
      const result = await createKey(name);
      setCreated(result);
    } catch {
      setCreateError('Could not create the key. Please try again.');
    }
  }

  async function handleRevoke(id: string) {
    setRevokeError(null);
    try {
      await revokeKey(id);
      setRevokeTarget(null);
    } catch {
      setRevokeError('Could not revoke the key. Please try again.');
    }
  }

  return (
    <div
      style={{
        padding: 'clamp(24px, 4vw, 40px) clamp(16px, 4vw, 28px) 96px',
        maxWidth: 1080,
        margin: '0 auto',
      }}
    >
      <div style={{ marginBottom: 28, minWidth: 0 }}>
        <h1
          style={{
            fontSize: 26,
            fontWeight: 600,
            color: 'var(--foreground)',
            margin: 0,
            letterSpacing: '-0.02em',
            lineHeight: 1.15,
          }}
        >
          API keys
        </h1>
        <p
          style={{
            fontSize: 14,
            color: 'var(--muted)',
            margin: '6px 0 0',
            maxWidth: 620,
            lineHeight: 1.55,
          }}
        >
          Ingest keys authenticate trace data sent to this workspace. Each key
          both proves the sender and decides which workspace the telemetry lands
          in.
        </p>
      </div>

      <SectionHead
        first
        title="Keys"
        hint="Active and revoked keys for this workspace, newest first."
        right={
          <div style={{ position: 'relative' }}>
            <span
              style={{
                position: 'absolute',
                left: 11,
                top: '50%',
                transform: 'translateY(-50%)',
                color: 'var(--muted)',
                pointerEvents: 'none',
              }}
            >
              <IconSearch size={13} />
            </span>
            <input
              aria-label="Search keys"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search keys…"
              style={{
                padding: '7px 10px 7px 32px',
                width: 220,
                background: 'transparent',
                border: '1px solid var(--border-strong)',
                borderRadius: 7,
                color: 'var(--foreground)',
                fontSize: 14,
                outline: 'none',
                fontFamily: 'inherit',
              }}
            />
          </div>
        }
      />

      {canCreate && (
        <InlineCreateForm
          key={created?.key.id ?? 'new'}
          onCreate={handleCreate}
          submitting={isCreating}
          error={createError}
        />
      )}

      {created && (
        <InlineReveal key={created.token} created={created} onClose={() => setCreated(null)} />
      )}

      {isLoading ? (
        <div style={{ padding: '60px 20px', display: 'grid', placeItems: 'center' }}>
          <Spinner />
        </div>
      ) : isError ? (
        <div
          style={{
            padding: '48px 20px',
            textAlign: 'center',
            color: 'var(--muted)',
            fontSize: 14,
          }}
          role="alert"
        >
          Could not load API keys for this workspace.
        </div>
      ) : (
        <div style={{ overflowX: 'auto' }}>
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: LIVE_TMPL,
              minWidth: 760,
              gap: 14,
              padding: '12px 0',
              borderBottom: '1px solid color-mix(in srgb, var(--border) 70%, transparent)',
              fontSize: 12,
              color: 'var(--muted)',
              textTransform: 'uppercase',
              letterSpacing: '0.06em',
              fontWeight: 500,
            }}
          >
            {LIVE_COLS.map((c, i) => (
              <div key={i} style={{ textAlign: c.align }}>
                {c.label}
              </div>
            ))}
          </div>

          {visible.length === 0 ? (
            <div
              style={{
                padding: '60px 20px',
                textAlign: 'center',
                color: 'var(--muted)',
                fontSize: 14,
              }}
            >
              {search
                ? `No keys match “${search}”`
                : 'No API keys yet. Create one to start sending traces.'}
            </div>
          ) : (
            visible.map((k, i) => (
              <React.Fragment key={k.id}>
                <LiveKeyRow
                  k={k}
                  isLast={i === visible.length - 1}
                  confirming={revokeTarget?.id === k.id}
                  onRevoke={(target) => {
                    setRevokeError(null);
                    setRevokeTarget(target);
                  }}
                />
                {revokeTarget?.id === k.id && (
                  <RevokeConfirmRow
                    target={revokeTarget}
                    onCancel={() => setRevokeTarget(null)}
                    onConfirm={handleRevoke}
                    submitting={isRevoking}
                    error={revokeError}
                  />
                )}
              </React.Fragment>
            ))
          )}
        </div>
      )}

      {!isLoading && !isError && keys.length > 0 && (
        <div style={{ marginTop: 16, fontSize: 13, color: 'var(--muted)' }}>
          {active.length} active · {revoked.length} revoked
        </div>
      )}
    </div>
  );
}

// Keyed by workspace so the one-time token and other view state never carry
// over to another workspace.
function LiveApiKeysPage({ workspaceId }: { workspaceId?: string }) {
  return <ApiKeysView key={workspaceId} data={useApiKeys(workspaceId)} canCreate={!!workspaceId} />;
}

function DemoApiKeysPage() {
  return <ApiKeysView data={useDemoApiKeys()} canCreate />;
}

export default function ApiKeysPage({ demo = false, workspaceId }: ApiKeysPageProps) {
  if (demo) return <DemoApiKeysPage />;
  return <LiveApiKeysPage workspaceId={workspaceId} />;
}
