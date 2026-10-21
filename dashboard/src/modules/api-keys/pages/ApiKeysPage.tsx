import React, { useState, useMemo } from 'react';
import {
  IconSearch,
  IconCopy,
  IconTrash,
  IconKey,
  IconCheck,
  IconAlert,
  IconPlus,
  StatusPill,
  Spinner,
  SlicedButton,
  SectionHead,
  formatDate,
  relativeTime,
  useCopy,
} from '../../../common';
import hover from '../../../common/styles/hover.module.css';
import { useApiKeys, useDemoApiKeys, useReadKeys } from '../hooks/useApiKeys';
import type { UseApiKeysResult } from '../hooks/useApiKeys';
import { API_VERSION, apiBaseUrl } from '../../../common/api';
import type { ApiKeyRecord, ApiKeyScope, CreatedApiKey } from '../../../common/api';

export interface ApiKeysPageProps {
  /** Seed the demo keys (auth-page preview). A real workspace starts with none. */
  demo?: boolean;
  /** The workspace to manage keys for, in the live (non-demo) page. */
  workspaceId?: string;
  workspaceName?: string;
}

function fmtLastUsed(iso: string | null): string {
  if (!iso) return 'Never';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  return relativeTime(d.getTime());
}

const MONO = 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace';

interface DraftKeyRowProps {
  kind: ApiKeyScope;
  scopeLabel: string;
  onCreate: (name: string) => void;
  onCancel: () => void;
  submitting: boolean;
  error: string | null;
}

function DraftKeyRow({ kind, scopeLabel, onCreate, onCancel, submitting, error }: DraftKeyRowProps) {
  const [name, setName] = useState('');
  const canCreate = name.trim().length > 0 && !submitting;

  return (
    <>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (canCreate) onCreate(name.trim());
        }}
        onKeyDown={(e) => {
          if (e.key === 'Escape') onCancel();
        }}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 12,
          padding: '10px 12px',
          margin: '8px 0',
          background: 'var(--surface-alt)',
          border: '1px solid var(--accent-border)',
          borderRadius: 8,
        }}
      >
        <span style={{ color: 'var(--accent)', display: 'flex', flexShrink: 0 }}>
          <IconKey size={15} />
        </span>
        <input
          aria-label="New key name"
          autoFocus
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={kind === 'read' ? 'e.g. Claude Code' : 'e.g. Production ingest'}
          style={{
            flex: 1,
            minWidth: 0,
            padding: '7px 0',
            background: 'transparent',
            border: 'none',
            outline: 'none',
            color: 'var(--foreground)',
            fontSize: 14.5,
            fontWeight: 500,
            fontFamily: 'inherit',
          }}
        />
        <span style={{ fontSize: 13, color: 'var(--muted)', whiteSpace: 'nowrap' }}>{scopeLabel}</span>
        <button
          type="button"
          onClick={onCancel}
          style={{
            padding: '6px 12px',
            background: 'transparent',
            border: '1px solid var(--border-strong)',
            borderRadius: 7,
            color: 'var(--foreground)',
            fontSize: 13.5,
            fontFamily: 'inherit',
            cursor: 'pointer',
          }}
        >
          Cancel
        </button>
        <SlicedButton type="submit" disabled={!canCreate} style={{ padding: '7px 20px 7px 14px', fontSize: 13.5 }}>
          {submitting ? 'Creating…' : 'Create'}
        </SlicedButton>
      </form>
      {error && (
        <div style={{ fontSize: 13.5, color: 'var(--error)', display: 'flex', gap: 8, alignItems: 'center' }}>
          <IconAlert size={14} />
          {error}
        </div>
      )}
    </>
  );
}

// splitBase turns the dashboard's own origin into the connector URL's editable
// parts; the host keeps its port.
function splitBase(base: string): { scheme: string; host: string } {
  try {
    const u = new URL(base);
    return { scheme: u.protocol === 'http:' ? 'http://' : 'https://', host: u.host };
  } catch {
    return { scheme: 'https://', host: '' };
  }
}

function validHost(scheme: string, host: string): boolean {
  if (!/^[^\s/?#]+$/.test(host.trim())) return false;
  try {
    const u = new URL(scheme + host.trim());
    return u.protocol === 'http:' || u.protocol === 'https:';
  } catch {
    return false;
  }
}

function KeyReveal({ created, onDone }: { created: CreatedApiKey; onDone: () => void }) {
  const [copyStatus, copy] = useCopy();
  const copied = copyStatus === 'copied';
  const copyFailed = copyStatus === 'failed';
  const isRead = created.key.scope === 'read';
  // A read key is handed over as the whole connector URL. The browser only knows
  // how it reached Tracium, not how clients will, so scheme and host are editable.
  const [{ scheme, host }, setAddress] = useState(() => splitBase(apiBaseUrl()));
  const [hostFocused, setHostFocused] = useState(false);
  const path = `/${API_VERSION}/mcp/${created.token}`;
  const hostOk = validHost(scheme, host);
  const secret = isRead ? scheme + host.trim() + path : created.token;

  return (
    <div
      style={{
        margin: '8px 0',
        padding: '16px 16px 14px',
        background: 'var(--surface-alt)',
        border: '1px solid var(--accent-border)',
        borderRadius: 8,
        display: 'flex',
        flexDirection: 'column',
        gap: 12,
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
        <span style={{ color: 'var(--accent)', display: 'flex' }}>
          <IconCheck size={15} stroke={2} />
        </span>
        <span style={{ fontSize: 14.5, fontWeight: 600, color: 'var(--foreground)' }}>{created.key.name}</span>
        <span style={{ fontSize: 13, color: 'var(--muted)' }}>Shown once.</span>
      </div>

      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 10,
          padding: '11px 12px 11px 14px',
          background: 'var(--background)',
          border: '1px solid var(--border-strong)',
          borderRadius: 8,
        }}
      >
        <span
          style={{
            flex: 1,
            fontFamily: MONO,
            fontSize: 13,
            lineHeight: isRead ? 2 : 1.5,
            wordBreak: 'break-all',
            color: 'var(--foreground)',
          }}
        >
          {isRead ? (
            <>
              <button
                type="button"
                title="Switch between http and https"
                className={hover.accent}
                onClick={() => setAddress({ scheme: scheme === 'https://' ? 'http://' : 'https://', host })}
                style={{
                  font: 'inherit',
                  background: 'none',
                  border: 'none',
                  borderBottom: '1px dotted var(--muted)',
                  padding: 0,
                  cursor: 'pointer',
                }}
              >
                {scheme}
              </button>
              <input
                aria-label="Tracium address"
                title="Where your MCP client reaches Tracium. claude.ai needs a public https address."
                value={host}
                onChange={(e) => setAddress({ scheme, host: e.target.value.replace(/^https?:\/\//i, '') })}
                onFocus={() => setHostFocused(true)}
                onBlur={() => setHostFocused(false)}
                size={Math.max(host.length, 4)}
                spellCheck={false}
                style={{
                  font: 'inherit',
                  color: 'var(--foreground)',
                  background: hostOk ? 'var(--accent-soft)' : 'color-mix(in srgb, var(--error) 10%, transparent)',
                  border: `1px ${hostFocused ? 'solid' : 'dashed'} ${hostOk ? 'var(--accent-border)' : 'var(--error)'}`,
                  boxShadow: hostFocused ? '0 0 0 3px var(--focus-ring)' : 'none',
                  borderRadius: 5,
                  padding: '2px 7px',
                  margin: '0 3px',
                  outline: 'none',
                }}
              />
              <span>{path}</span>
            </>
          ) : (
            secret
          )}
        </span>
        <button
          type="button"
          onClick={() => copy(secret)}
          disabled={isRead && !hostOk}
          style={{
            padding: '6px 11px',
            background: copied ? 'var(--accent)' : 'transparent',
            border: '1px solid ' + (copied ? 'var(--accent)' : copyFailed ? 'var(--error)' : 'var(--border-strong)'),
            borderRadius: 6,
            color: copied ? 'var(--accent-contrast)' : copyFailed ? 'var(--error)' : 'var(--foreground)',
            fontSize: 12.5,
            fontWeight: 500,
            fontFamily: 'inherit',
            flexShrink: 0,
            cursor: isRead && !hostOk ? 'not-allowed' : 'pointer',
            opacity: isRead && !hostOk ? 0.5 : 1,
          }}
        >
          {copied ? 'Copied' : copyFailed ? 'Copy failed' : 'Copy'}
        </button>
      </div>
      {copyFailed && (
        <div style={{ fontSize: 13, color: 'var(--error)' }}>
          Select it and copy it manually.
        </div>
      )}

      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 16, flexWrap: 'wrap' }}>
        <div style={{ fontSize: 13, color: isRead && !hostOk ? 'var(--error)' : 'var(--muted)' }}>
          {!isRead ? 'Bearer token for OTLP ingest.' : hostOk ? 'Paste into your MCP client.' : 'Enter a host, without a path.'}
        </div>
        <SlicedButton onClick={onDone} style={{ padding: '7px 20px 7px 14px', fontSize: 13.5 }}>
          Done
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
        {target.scope === 'read' ? 'Requests' : 'Ingest requests'} presenting{' '}
        <code style={{ fontFamily: 'var(--font-mono)' }}>{target.prefix}…</code>{' '}
        {target.scope === 'read'
          ? 'will be rejected right away.'
          : "will be rejected shortly after (subject to the collector's short verification cache)."}{' '}
        This cannot be undone.
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
interface ApiKeysViewProps {
  data: UseApiKeysResult;
  canCreate: boolean;
  kind?: ApiKeyScope;
  /** Renders just the key list section, for embedding in another page. */
  embedded?: boolean;
  workspaceName?: string;
}

type Step = 'idle' | 'form' | 'reveal';

function ApiKeysView({ data, canCreate, kind = 'ingest', embedded = false, workspaceName }: ApiKeysViewProps) {
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
  const [step, setStep] = useState<Step>('idle');
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
      setStep('reveal');
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
      style={
        embedded
          ? undefined
          : {
              padding: 'clamp(24px, 4vw, 40px) clamp(16px, 4vw, 28px) 96px',
              maxWidth: 1080,
              margin: '0 auto',
            }
      }
    >
      {!embedded && <div style={{ marginBottom: 28, minWidth: 0 }}>
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
          Keys that send traces to {workspaceName ?? 'this workspace'}.
        </p>
      </div>}

      <SectionHead
        first={!embedded}
        title={kind === 'read' ? 'Read keys' : 'Keys'}
        right={
          <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
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
            <SlicedButton
              onClick={() => {
                setCreateError(null);
                setStep('form');
              }}
              disabled={!canCreate || step !== 'idle' || isLoading || isError}
              style={{ display: 'flex', alignItems: 'center', gap: 7, padding: '8px 22px 8px 14px' }}
            >
              <IconPlus size={14} stroke={2} />
              Create key
            </SlicedButton>
          </div>
        }
      />

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
          Could not load API keys.
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

          {step === 'form' && (
            <DraftKeyRow
              kind={kind}
              scopeLabel={kind === 'read' ? 'Reads your workspaces' : `Ingests to ${workspaceName ?? 'this workspace'}`}
              onCreate={handleCreate}
              onCancel={() => setStep('idle')}
              submitting={isCreating}
              error={createError}
            />
          )}
          {step === 'reveal' && created && (
            <KeyReveal
              key={created.token}
              created={created}
              onDone={() => {
                setCreated(null);
                setStep('idle');
              }}
            />
          )}

          {visible.length === 0 ? (
            // The draft row or reveal panel stands in for the empty message.
            step === 'idle' && (
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
                  : kind === 'read'
                    ? 'No read keys yet. Create one to connect an MCP client.'
                    : 'No API keys yet. Create one to start sending traces.'}
              </div>
            )
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
function LiveApiKeysPage({ workspaceId, workspaceName }: { workspaceId?: string; workspaceName?: string }) {
  return (
    <ApiKeysView key={workspaceId} data={useApiKeys(workspaceId)} canCreate={!!workspaceId} workspaceName={workspaceName} />
  );
}

// ReadKeysSection lists the signed-in account's read keys, for account settings.
export function ReadKeysSection() {
  return <ApiKeysView kind="read" embedded data={useReadKeys()} canCreate />;
}

function DemoApiKeysPage({ workspaceName }: { workspaceName?: string }) {
  return <ApiKeysView data={useDemoApiKeys()} canCreate workspaceName={workspaceName} />;
}

export default function ApiKeysPage({ demo = false, workspaceId, workspaceName }: ApiKeysPageProps) {
  if (demo) return <DemoApiKeysPage workspaceName={workspaceName} />;
  return <LiveApiKeysPage workspaceId={workspaceId} workspaceName={workspaceName} />;
}
