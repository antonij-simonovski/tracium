import type { SettingsSection } from '../../../extensions';

import React, { useEffect, useRef, useState } from 'react';
import type { TabId } from '../ids';
import type { Workspace } from '../../shell/interfaces';
import type { Account } from '../../auth';
import {
  IconUser,
  IconBuilding,
  IconTrash,
  IconCheck,
  IconCopy,
  IconPlus,
  IconUsers,
  IconAlert,
  SectionHead,
  formatDate,
  useCopy,
  useMaxWidth,
  BREAKPOINTS,
  SlicedButton,
} from '../../../common';
import { useAPIClient } from '../../../common/providers/APIProvider';
import { ReadKeysSection } from '../../api-keys';
import { useIncidentBacktest } from '../../overview/hooks/useMetrics';
import { APIError, inviteLink, type AlertChannel, type AlertRule, type AlertTestResult, type CreatedInvite, type WorkspaceMember } from '../../../common/api';
import { useQuery, useQueryClient } from '@tanstack/react-query';

export interface WorkspaceDraft {
  name: string;
  slug: string;
  env: Workspace['env'];
}

export interface SettingsPageProps {
  sections?: readonly SettingsSection[];
  /** Persists a new workspace and selects it. Provided by the Dashboard shell. */
  createWorkspace?: (draft: WorkspaceDraft) => Promise<Workspace>;
  onOpenOverview?: () => void;
  /** Opens the API keys screen for the selected workspace. */
  onOpenApiKeys?: () => void;
  /** OTLP endpoint shown in the connection instructions. Defaults to the local collector. */
  ingestEndpoint?: string;
  onCancelCreate?: () => void;
  /** When true, the Workspace tab opens on a create-workspace form. */
  createMode?: boolean;
  /** The tab to open on, from a deep link such as /settings/alerts. */
  initialTab?: TabId;
  /** When true, render the rich demo data (logged-out auth-page preview). */
  demo?: boolean;
  /** The real signed-in workspace. Used when not in demo mode. */
  workspace?: Workspace | null;
  /** The real signed-in account. Used when not in demo mode. */
  account?: Account | null;
  /** Stores the session token returned by a password change. */
  onSessionRenewed?: (token: string) => void;
}

interface SettingsUser {
  name: string;
  email: string;
  initials: string;
  role: string;
  joined: string;
}

interface SettingsWorkspace {
  name: string;
  id: string;
  defaultRetention: string;
  members: number;
}

const SET_USER: SettingsUser = {
  name: 'Mark Gonzales',
  email: 'mark@umbrella.dev',
  initials: 'MG',
  role: 'Owner',
  joined: 'Jan 14, 2025',
};

const SET_WORKSPACE: SettingsWorkspace = {
  name: 'umbrella',
  id: 'ws_2qHv4Rt81xLpKnvR',
  defaultRetention: '30 days',
  members: 12,
};

interface TabDef {
  id: TabId;
  label: string;
  icon: React.ReactNode;
}

const SET_TABS: TabDef[] = [
  { id: 'account',       label: 'Account',        icon: <IconUser size={14} /> },
  { id: 'workspace',     label: 'Workspace',      icon: <IconBuilding size={14} /> },
  { id: 'members',       label: 'Members',        icon: <IconUsers size={14} /> },
  { id: 'alerts',        label: 'Alerts',         icon: <IconAlert size={14} /> },
  { id: 'danger',        label: 'Danger zone',    icon: <IconTrash size={14} /> },
];

interface FieldProps {
  label: string;
  hint?: string;
  children: React.ReactNode;
  right?: React.ReactNode;
  last?: boolean;
}

function Field({ label, hint, children, right, last = false }: FieldProps) {
  // On phones the label / control / action columns stack vertically instead of
  // squeezing into a three-column grid.
  const stacked = useMaxWidth(BREAKPOINTS.mobile);
  return (
    <div style={{
      display: 'grid',
      gridTemplateColumns: stacked ? '1fr' : 'minmax(180px, 220px) 1fr auto',
      gap: stacked ? 12 : 32, alignItems: 'start',
      padding: '20px 0',
      borderBottom: last ? 'none' : '1px solid color-mix(in srgb, var(--border) 50%, transparent)',
    }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        <span style={{ fontSize: 14, fontWeight: 500, color: 'var(--foreground)' }}>{label}</span>
        {hint && <span style={{ fontSize: 13, color: 'var(--muted)', lineHeight: 1.5 }}>{hint}</span>}
      </div>
      <div style={{ minWidth: 0 }}>{children}</div>
      <div style={{ justifySelf: stacked ? 'start' : 'end', display: 'flex', alignItems: 'center', gap: 8 }}>{right}</div>
    </div>
  );
}

type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger';

interface Btn {
  children: React.ReactNode;
  variant?: ButtonVariant;
  onClick?: () => void;
  disabled?: boolean;
  type?: 'button' | 'submit' | 'reset';
  style?: React.CSSProperties;
  title?: string;
}

function Btn({ children, variant = 'secondary', onClick, disabled, type = 'button', style, title }: Btn) {
  if (variant === 'primary') {
    return (
      <SlicedButton type={type} title={title} onClick={onClick} disabled={disabled}
        style={{ display: 'inline-flex', alignItems: 'center', gap: 6, padding: '7px 20px 7px 13px', ...style }}>{children}</SlicedButton>
    );
  }
  const variantStyles: Record<Exclude<ButtonVariant, 'primary'>, React.CSSProperties> = {
    secondary: { background: 'transparent', color: 'var(--foreground)', border: '1px solid var(--border-strong)', fontWeight: 500 },
    ghost:     { background: 'transparent', color: 'var(--muted)', border: '1px solid transparent', fontWeight: 500 },
    danger:    { background: 'color-mix(in srgb, var(--error) 12%, transparent)', color: 'var(--error)', border: '1px solid color-mix(in srgb, var(--error) 30%, transparent)', fontWeight: 500 },
  };
  return (
    <button type={type} title={title} onClick={onClick} disabled={disabled} style={{
      display: 'inline-flex', alignItems: 'center', gap: 6,
      padding: '7px 13px', borderRadius: 7,
      fontSize: 14, fontFamily: 'inherit',
      cursor: disabled ? 'not-allowed' : 'pointer',
      opacity: disabled ? 0.5 : 1,
      ...variantStyles[variant], ...style,
    }}>{children}</button>
  );
}

interface InputProps {
  value: string;
  onChange: (e: React.ChangeEvent<HTMLInputElement>) => void;
  placeholder?: string;
  type?: string;
  mono?: boolean;
  prefix?: React.ReactNode;
  suffix?: React.ReactNode;
  readOnly?: boolean;
  label?: string;
  autoComplete?: string;
}

function Input({ value, onChange, placeholder, type = 'text', mono = false, prefix, suffix, readOnly, label, autoComplete }: InputProps) {
  return (
    <div
      style={{
        display: 'flex', alignItems: 'center', gap: 8,
        padding: '0 11px',
        background: readOnly ? 'var(--surface-alt)' : 'transparent',
        border: '1px solid var(--border-strong)',
        borderRadius: 7,
        transition: 'border-color 120ms',
      }}
      onFocusCapture={e => { e.currentTarget.style.borderColor = 'color-mix(in srgb, var(--accent) 50%, transparent)'; }}
      onBlurCapture={e => { e.currentTarget.style.borderColor = 'var(--border-strong)'; }}
    >
      {prefix && <span style={{ fontSize: 14, color: 'var(--muted)', flexShrink: 0 }}>{prefix}</span>}
      <input
        aria-label={label}
        value={value}
        onChange={onChange}
        placeholder={placeholder}
        type={type}
        readOnly={readOnly}
        autoComplete={autoComplete}
        style={{
          flex: 1, minWidth: 0, padding: '9px 0',
          background: 'transparent', border: 'none', outline: 'none',
          color: 'var(--foreground)',
          fontSize: 14,
          fontFamily: mono ? 'var(--font-mono)' : 'inherit',
        }}
      />
      {suffix && <span style={{ flexShrink: 0 }}>{suffix}</span>}
    </div>
  );
}

interface ToggleProps {
  on: boolean;
  onChange: (v: boolean) => void;
  label?: string;
}

function Toggle({ on, onChange, label }: ToggleProps) {
  return (
    <button
      type="button"
      onClick={() => onChange(!on)}
      role="switch"
      aria-checked={on}
      aria-label={label}
      style={{
        width: 36, height: 20,
        background: on ? 'var(--accent)' : 'var(--surface-alt)',
        border: '1px solid ' + (on ? 'var(--accent)' : 'var(--border-strong)'),
        borderRadius: 999,
        position: 'relative',
        transition: 'background 150ms, border-color 150ms',
        padding: 0, cursor: 'pointer',
      }}
    >
      <span style={{
        position: 'absolute', top: 1, left: on ? 17 : 1,
        width: 16, height: 16, borderRadius: 999,
        background: on ? 'var(--accent-contrast)' : 'var(--foreground)',
        transition: 'left 150ms',
      }} />
    </button>
  );
}

interface SegmentedOption {
  id: string;
  label: string;
  icon?: React.ReactNode;
}

interface SegmentedProps {
  value: string;
  onChange: (v: string) => void;
  options: SegmentedOption[];
}

function Segmented({ value, onChange, options }: SegmentedProps) {
  return (
    <div style={{ display: 'inline-flex', flexWrap: 'wrap', background: 'var(--surface-alt)', border: '1px solid var(--border)', borderRadius: 7, padding: 2 }}>
      {options.map(o => (
        <button type="button" aria-pressed={value === o.id} key={o.id} onClick={() => onChange(o.id)} style={{
          padding: '5px 11px',
          fontSize: 13, fontWeight: 500, fontFamily: 'inherit',
          background: value === o.id ? 'var(--surface)' : 'transparent',
          color: value === o.id ? 'var(--foreground)' : 'var(--muted)',
          border: '1px solid ' + (value === o.id ? 'var(--border)' : 'transparent'),
          borderRadius: 5,
          display: 'inline-flex', alignItems: 'center', gap: 6,
          cursor: 'pointer',
        }}>
          {o.icon}
          {o.label}
        </button>
      ))}
    </div>
  );
}

interface TabRailProps {
  sections: readonly SettingsSection[];
  tab: TabId;
  setTab: (t: TabId) => void;
  /** Renders the rail as a horizontally scrolling strip (stacked layouts). */
  horizontal?: boolean;
  demo?: boolean;
}

function TabRail({ sections, tab, setTab, horizontal = false, demo = false }: TabRailProps) {
  return (
    <aside style={{
      ...(horizontal
        ? { width: '100%', overflowX: 'auto' }
        : { width: 220, flexShrink: 0, position: 'sticky', top: 73, alignSelf: 'flex-start' }),
      paddingTop: 4,
    }}>
      {!horizontal && (
        <div style={{ fontSize: 12, color: 'var(--muted)', textTransform: 'uppercase', letterSpacing: '0.08em', fontWeight: 500, padding: '0 10px 10px' }}>Settings</div>
      )}
      <nav style={{ display: 'flex', flexDirection: horizontal ? 'row' : 'column', gap: horizontal ? 4 : 1 }}>
        {[...SET_TABS.filter(t => demo ? t.id !== 'members' && t.id !== 'alerts' : t.id !== 'danger'), ...sections.map(t => ({...t, icon: t.icon ?? null}))].map(t => {
          const active = t.id === tab;
          const danger = t.id === 'danger';
          return (
            <button
              key={t.id}
              onClick={() => setTab(t.id)}
              style={{
                display: 'flex', alignItems: 'center', gap: 9,
                padding: '8px 10px',
                fontSize: 14, fontWeight: 500, fontFamily: 'inherit',
                color: active ? (danger ? 'var(--error)' : 'var(--foreground)') : 'var(--muted)',
                background: active ? 'var(--surface)' : 'transparent',
                border: '1px solid ' + (active ? 'var(--border)' : 'transparent'),
                borderRadius: 7,
                textAlign: 'left',
                cursor: 'pointer',
                transition: 'color .12s, background .12s',
                ...(horizontal ? { flexShrink: 0, whiteSpace: 'nowrap' } : {}),
              }}
              onMouseEnter={e => { if (!active) e.currentTarget.style.color = danger ? 'var(--error)' : 'var(--foreground)'; }}
              onMouseLeave={e => { if (!active) e.currentTarget.style.color = 'var(--muted)'; }}
            >
              <span style={{ flexShrink: 0, opacity: active ? 1 : 0.7 }}>{t.icon}</span>
              <span style={{ flex: 1 }}>{t.label}</span>
            </button>
          );
        })}
      </nav>
    </aside>
  );
}

interface AccountViewProps {
  user: SettingsUser;
  demo: boolean;
  onSessionRenewed?: (token: string) => void;
}

function ChangePasswordField({ onSessionRenewed }: { onSessionRenewed?: (token: string) => void }) {
  const { usersAPI } = useAPIClient();
  const [editing, setEditing] = useState(false);
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [justChanged, setJustChanged] = useState(false);

  const reset = () => {
    setEditing(false);
    setCurrent('');
    setNext('');
    setConfirm('');
    setError('');
  };

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (saving) return;
    if (next !== confirm) {
      setError("New passwords don't match.");
      return;
    }
    setSaving(true);
    setError('');
    try {
      const token = await usersAPI.changePassword(current, next);
      onSessionRenewed?.(token);
      reset();
      setJustChanged(true);
    } catch (err) {
      const message = err instanceof APIError ? err.message : 'Could not change password. Please try again.';
      setError(message);
    } finally {
      setSaving(false);
    }
  };

  if (!editing) {
    return (
      <Field
        label="Password"
        hint={justChanged ? 'Password updated.' : undefined}
        right={<Btn variant="secondary" onClick={() => { setJustChanged(false); setEditing(true); }}>Change</Btn>}
        last
      >
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: 14, color: 'var(--muted)', letterSpacing: '0.2em' }}>•••••••••••</span>
      </Field>
    );
  }

  return (
    <form onSubmit={submit} aria-busy={saving}>
      <fieldset disabled={saving} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}>
        <Field label="Current password">
          <Input label="Current password" type="password" autoComplete="current-password" value={current} onChange={e => setCurrent(e.target.value)} />
        </Field>
        <Field label="New password" hint="At least 8 characters.">
          <Input label="New password" type="password" autoComplete="new-password" value={next} onChange={e => setNext(e.target.value)} />
        </Field>
        <Field label="Confirm new password" last>
          <Input label="Confirm new password" type="password" autoComplete="new-password" value={confirm} onChange={e => setConfirm(e.target.value)} />
        </Field>
      </fieldset>
      {error && <p role="alert" style={{ color: 'var(--error)', fontSize: 14 }}>{error}</p>}
      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, paddingTop: 18 }}>
        <Btn variant="ghost" disabled={saving} onClick={reset}>Cancel</Btn>
        <Btn variant="primary" type="submit" disabled={saving || !current || !next || !confirm}>
          {saving ? 'Saving…' : 'Save password'}
        </Btn>
      </div>
    </form>
  );
}

function AccountView({ user, demo, onSessionRenewed }: AccountViewProps) {
  const [tabnav, setTabnav] = useState(true);
  if (!demo) return <div>
    <SectionHead first title="Account" hint="Your sign-in identity." />
    <Field label="Email" last><span>{user.email}</span></Field>

    <SectionHead title="Security" hint="Change the password used to sign in." />
    <ChangePasswordField onSessionRenewed={onSessionRenewed} />

    <ReadKeysSection />
  </div>;

  return (
    <div>
      <SectionHead
        first
        title="Profile"
        hint="How you appear in audit logs, comments, and trace assignments across this workspace."
      />
      <div>

        <Field
          label="Email"
          hint="Used for sign-in."
          right={<Btn variant="ghost">Change email</Btn>}
          last
        >
          <Input
            value={user.email}
            onChange={() => undefined}
            readOnly
          />
        </Field>
      </div>

      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, paddingTop: 18 }}>
        <Btn variant="ghost">Cancel</Btn>
        <Btn variant="primary">Save profile</Btn>
      </div>

      <SectionHead title="Security" hint="Sign-in factors, active sessions, and recent activity on your account." />
      <div>
        <Field label="Password" hint={demo ? 'Last changed 84 days ago.' : 'Set when you created your account.'} right={<Btn variant="secondary">Change</Btn>} last>
          <span style={{ fontFamily: 'var(--font-mono)', fontSize: 14, color: 'var(--muted)', letterSpacing: '0.2em' }}>•••••••••••</span>
        </Field>
      </div>

      <SectionHead title="Display" hint="Personal preferences. Stored on this device." />
      <div>
        <Field label="Keyboard navigation" hint="Use J / K to move through trace lists; / to focus search." last>
          <Toggle on={tabnav} onChange={setTabnav} />
        </Field>
      </div>
    </div>
  );
}

function slugify(s: string): string {
  return s.toLowerCase().trim().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
}

interface CreateWorkspaceViewProps {
  onCreate: (draft: WorkspaceDraft) => Promise<void>;
  onCancel: () => void;
}

function CreateWorkspaceView({ onCreate, onCancel }: CreateWorkspaceViewProps) {
  const [name, setName] = useState('');
  const [slug, setSlug] = useState('');
  const [slugEdited, setSlugEdited] = useState(false);
  const [env, setEnv] = useState<Workspace['env']>('production');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState('');
  const submitting = useRef(false);

  const effectiveSlug = slugEdited ? slug : slugify(name);
  const canCreate = name.trim().length > 0 && effectiveSlug.length > 0;

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!canCreate || submitting.current) return;
    submitting.current = true;
    setPending(true);
    setError('');
    try {
      await onCreate({ name: name.trim(), slug: effectiveSlug, env });
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not create workspace. Please try again.');
    } finally {
      submitting.current = false;
      setPending(false);
    }
  };

  return (
    <form onSubmit={submit} aria-busy={pending}>
      <p style={{ color: 'var(--muted)', fontSize: 13, margin: '0 0 16px' }}>Step 1 of 2 · Workspace details</p>
      <SectionHead
        first
        title="Create workspace"
        hint="First, name your workspace. Next, we’ll show you how to connect your application."
      />
      <fieldset disabled={pending} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}>
        <Field label="Workspace name" hint="Shown in the workspace switcher and on invites.">
          <Input label="Workspace name" value={name} onChange={e => setName(e.target.value)} placeholder="Acme Production" />
        </Field>

        <Field label="Slug" hint="A readable label. Applications send data with an API key, not the slug.">
          <Input
            label="Slug"
            value={effectiveSlug}
            onChange={e => { setSlugEdited(true); setSlug(slugify(e.target.value)); }}
            placeholder="acme-production"
            mono
          />
        </Field>

        <Field label="Environment" hint="Labels the workspace in the switcher." last>
          <Segmented value={env} onChange={v => setEnv(v as Workspace['env'])} options={[
            { id: 'production',  label: 'Production' },
            { id: 'staging',     label: 'Staging' },
            { id: 'development', label: 'Development' },
          ]} />
        </Field>
      </fieldset>
      {error && <p role="alert" style={{ color: 'var(--error)', fontSize: 14 }}>{error}</p>}

      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, paddingTop: 18 }}>
        <Btn variant="ghost" disabled={pending} onClick={onCancel}>Cancel</Btn>
        <Btn variant="primary" disabled={!canCreate || pending} type="submit">
          <IconPlus size={13} />
          {pending ? 'Creating…' : 'Create workspace & continue'}
        </Btn>
      </div>
    </form>
  );
}

interface WorkspaceViewProps {
  ws: SettingsWorkspace;
  demo?: boolean;
  justCreated?: boolean;
  onOpenOverview?: () => void;
  onOpenApiKeys?: () => void;
  ingestEndpoint?: string;
}

function CopyButton({ value, label }: { value: string; label: string }) {
  const [status, copy] = useCopy();
  const message = status === 'failed' ? 'Couldn’t copy. Select and copy the text manually.' : status === 'copied' ? 'Copied to clipboard.' : '';
  return <span style={{ display: 'inline-flex', flexDirection: 'column', alignItems: 'flex-start', gap: 6 }}>
    <Btn onClick={() => copy(value)}>
      {status === 'copied' ? <IconCheck size={13} /> : <IconCopy size={13} />}
      {status === 'copied' ? 'Copied' : label}
    </Btn>
    <span role="status" style={{ fontSize: 13, color: status === 'failed' ? 'var(--error)' : 'var(--muted)' }}>
      {message}
    </span>
  </span>;
}

const LOCAL_INGEST_ENDPOINT = 'http://localhost:4318';

const exporterConfig = (endpoint: string) => [
  `OTEL_EXPORTER_OTLP_ENDPOINT=${endpoint}`,
  'OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf',
  'OTEL_EXPORTER_OTLP_HEADERS="Authorization=Bearer YOUR_API_KEY"',
].join('\n');

function WorkspaceConnection({ ws, justCreated, onOpenOverview, onOpenApiKeys, ingestEndpoint }: WorkspaceViewProps) {
  const config = exporterConfig(ingestEndpoint || LOCAL_INGEST_ENDPOINT);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    if (justCreated) heading.current?.focus();
  }, [justCreated]);
  const divider = '1px solid color-mix(in srgb, var(--border) 50%, transparent)';
  return <div>
    {justCreated && <p role="status" style={{ display: 'flex', alignItems: 'center', gap: 8, color: 'var(--accent)', fontSize: 14, margin: '0 0 16px' }}>
      <IconCheck size={14} /> {ws.name} created · Step 2 of 2
    </p>}
    <SectionHead first title="Connect your application" headingRef={heading} style={{ paddingBottom: 8 }} />
    <p style={{ fontSize: 14, color: 'var(--muted)', lineHeight: 1.6, margin: '0 0 24px', maxWidth: 620 }}>
      Applications send traces with an API key. Each key belongs to one workspace, so traces sent with a key from <strong style={{ color: 'var(--foreground)' }}>{ws.name}</strong> land here.
    </p>
    <h3 style={{ fontSize: 15, fontWeight: 600, margin: '0 0 8px' }}>1. Create an API key</h3>
    <p style={{ fontSize: 14, color: 'var(--muted)', lineHeight: 1.6, margin: '0 0 12px' }}>Create a key for this workspace and copy its <code>trc_…</code> token. It is shown only once. Use a separate key for each application so you can revoke them independently.</p>
    {onOpenApiKeys && <div style={{ marginBottom: 12 }}><Btn onClick={onOpenApiKeys}>Create API key</Btn></div>}
    <div style={{ paddingBottom: 12, borderBottom: divider }} />
    <h3 style={{ fontSize: 15, fontWeight: 600, margin: '24px 0 8px' }}>2. Configure your OpenTelemetry exporter</h3>
    <p style={{ fontSize: 14, color: 'var(--muted)', lineHeight: 1.6, margin: '0 0 12px' }}>Set these environment variables where your application runs, replacing <code>YOUR_API_KEY</code> with the token.{!ingestEndpoint && ' The endpoint shown is for an application on the host of a local Docker installation; for other deployments, use the collector address provided by your operator.'}</p>
    <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: '12px 24px', flexWrap: 'wrap', padding: '8px 0 24px', borderBottom: divider }}>
      <pre style={{ minWidth: 0, flex: '1 1 280px', margin: 0, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', fontSize: 13, lineHeight: 1.7, fontFamily: 'var(--font-mono)' }}>{config}</pre>
      <CopyButton value={config} label="Copy configuration" />
    </div>
    <h3 style={{ fontSize: 15, fontWeight: 600, margin: '24px 0 8px' }}>3. Send your first trace</h3>
    <p style={{ fontSize: 14, color: 'var(--muted)', lineHeight: 1.6, margin: 0 }}>Restart your instrumented application and run a request. Requests without a valid key are rejected with <code>401</code> and nothing is stored.</p>
    <p style={{ fontSize: 13, color: 'var(--muted)', lineHeight: 1.6, margin: '16px 0 0' }}>You can always find these instructions in <strong style={{ color: 'var(--foreground)' }}>Settings → Workspace</strong>.</p>
    {onOpenOverview && <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 24 }}>
      <Btn variant="primary" onClick={onOpenOverview}>Go to overview</Btn>
    </div>}
  </div>;
}

function WorkspaceView({ ws, demo = false, justCreated = false, onOpenOverview, onOpenApiKeys, ingestEndpoint }: WorkspaceViewProps) {
  const [name, setName] = useState(ws.name);
  const [retention, setRetention] = useState(ws.defaultRetention);

  if (!demo) return <div>
    {ws.id ? <WorkspaceConnection ws={ws} justCreated={justCreated} onOpenOverview={onOpenOverview} onOpenApiKeys={onOpenApiKeys} ingestEndpoint={ingestEndpoint} /> : <SectionHead first title="Workspace" hint="Create a workspace to get its connection instructions." />}
    {!justCreated && <>
      <SectionHead title="Retention" hint="Retention is configured by the deployment operator. Expired spans are deleted; daily aggregates are retained separately." />
      <p style={{ fontSize: 14, color: 'var(--muted)' }}>Workspace editing and retention controls are not yet available.</p>
    </>}
  </div>;

  return (
    <div>
      <SectionHead
        first
        title="Identity"
        hint="The display name your team uses to recognise this workspace."
      />
      <div>
        <Field label="Workspace name" hint="Shown in the workspace switcher and on invites." last>
          <Input value={name} onChange={e => setName(e.target.value)} />
        </Field>
      </div>

      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, paddingTop: 18 }}>
        <Btn variant="ghost">Cancel</Btn>
        <Btn variant="primary">Save changes</Btn>
      </div>

      <SectionHead title="Data & ingestion" hint="Controls applied to every trace ingested into this workspace." />
      <div>
        <Field label="Trace retention" hint="Older spans are moved to cold storage and excluded from search." last>
          <Segmented value={retention} onChange={setRetention} options={[
            { id: '7 days',   label: '7 days' },
            { id: '30 days',  label: '30 days' },
            { id: '90 days',  label: '90 days' },
            { id: '365 days', label: '1 year' },
          ]} />
        </Field>
      </div>
    </div>
  );
}

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof APIError ? err.message : fallback;
}

interface MembersViewProps {
  workspace: Workspace;
  account: Account | null;
}

function MembersView({ workspace, account }: MembersViewProps) {
  const { workspacesAPI } = useAPIClient();
  const queryClient = useQueryClient();
  const isOwner = workspace.role.toLowerCase() === 'owner';
  const membersKey = ['workspace-members', workspace.id];
  const invitesKey = ['workspace-invites', workspace.id];

  const members = useQuery({ queryKey: membersKey, queryFn: () => workspacesAPI.members(workspace.id) });
  const invites = useQuery({ queryKey: invitesKey, queryFn: () => workspacesAPI.invites(workspace.id), enabled: isOwner });

  const [email, setEmail] = useState('');
  const [inviting, setInviting] = useState(false);
  const [inviteError, setInviteError] = useState('');
  const [created, setCreated] = useState<CreatedInvite | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [actionError, setActionError] = useState('');

  const refreshInvites = () => void queryClient.invalidateQueries({ queryKey: invitesKey });
  const refreshMembers = () => {
    void queryClient.invalidateQueries({ queryKey: membersKey });
    // The workspace switcher shows each workspace's member count.
    void queryClient.invalidateQueries({ queryKey: ['workspaces'] });
  };

  const sendInvite = async (address: string) => {
    setInviting(true);
    setInviteError('');
    setActionError('');
    try {
      const invite = await workspacesAPI.invite(workspace.id, address.trim());
      setCreated(invite);
      setEmail('');
      refreshInvites();
    } catch (err) {
      setInviteError(errorMessage(err, 'Could not create the invite. Please try again.'));
    } finally {
      setInviting(false);
    }
  };

  const runAction = async (id: string, action: () => Promise<void>, onDone: () => void, fallback: string) => {
    setBusyId(id);
    setActionError('');
    try {
      await action();
      onDone();
    } catch (err) {
      setActionError(errorMessage(err, fallback));
    } finally {
      setBusyId(null);
    }
  };

  const divider = '1px solid color-mix(in srgb, var(--border) 50%, transparent)';
  const row: React.CSSProperties = { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '8px 24px', flexWrap: 'wrap', padding: '14px 0', borderBottom: divider };
  const meta: React.CSSProperties = { fontSize: 13, color: 'var(--muted)' };
  const self = account?.email.toLowerCase();

  return <div>
    <SectionHead
      first
      title="Members"
      hint={isOwner
        ? `Everyone who can see ${workspace.name}’s traces, usage and API keys. Invite people by email and share the link with them.`
        : `Everyone who can see ${workspace.name}’s traces, usage and API keys. Only the workspace owner can invite or remove members.`}
    />

    {isOwner && <form onSubmit={e => { e.preventDefault(); if (email.trim()) void sendInvite(email); }} aria-busy={inviting}>
      <fieldset disabled={inviting} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}>
        <Field label="Invite by email" hint="They’ll join as a member. The link works once and expires after 7 days." last
          right={<Btn variant="primary" type="submit" disabled={inviting || !email.trim()}><IconPlus size={13} />{inviting ? 'Creating…' : 'Create invite link'}</Btn>}>
          <Input label="Invite email" type="email" value={email} onChange={e => setEmail(e.target.value)} placeholder="teammate@example.com" />
        </Field>
      </fieldset>
      {inviteError && <p role="alert" style={{ color: 'var(--error)', fontSize: 14, margin: '0 0 12px' }}>{inviteError}</p>}
    </form>}

    {created && <div role="status" style={{ padding: '14px 16px', margin: '4px 0 8px', borderRadius: 8, border: '1px solid color-mix(in srgb, var(--accent) 35%, transparent)', background: 'color-mix(in srgb, var(--accent) 8%, transparent)' }}>
      <p style={{ fontSize: 14, margin: '0 0 10px', lineHeight: 1.55 }}>
        {created.email_sent
          ? <>We emailed an invite to <strong>{created.email}</strong>. You can also send them this link. They need to sign in or sign up with that address to accept it. The link is shown only once.</>
          : <>Send this link to <strong>{created.email}</strong>. They need to sign in or sign up with that address to accept it. The link is shown only once.</>}
      </p>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: '8px 16px', flexWrap: 'wrap' }}>
        <code style={{ flex: '1 1 280px', minWidth: 0, overflowWrap: 'anywhere', fontSize: 13, fontFamily: 'var(--font-mono)' }}>{inviteLink(created.token)}</code>
        <CopyButton key={created.token} value={inviteLink(created.token)} label="Copy invite link" />
      </div>
      <div style={{ marginTop: 4 }}><Btn variant="ghost" onClick={() => setCreated(null)}>Done</Btn></div>
    </div>}

    {actionError && <p role="alert" style={{ color: 'var(--error)', fontSize: 14 }}>{actionError}</p>}

    <SectionHead title="People" />
    {members.isLoading && <p style={meta}>Loading members…</p>}
    {members.isError && <p role="alert" style={{ color: 'var(--error)', fontSize: 14 }}>{errorMessage(members.error, 'Could not load members.')}</p>}
    {members.data && <ul aria-label="Members" style={{ listStyle: 'none', margin: 0, padding: 0 }}>
      {members.data.map((m: WorkspaceMember) => {
        const you = m.email.toLowerCase() === self;
        return <li key={m.user_id} style={row}>
          <div style={{ minWidth: 0 }}>
            <div style={{ fontSize: 14.5, fontWeight: 500, overflowWrap: 'anywhere' }}>{m.email}{you && <span style={{ ...meta, marginLeft: 8 }}>You</span>}</div>
            <div style={meta}>{m.role === 'owner' ? 'Owner' : 'Member'} · joined {formatDate(m.joined_at)}</div>
          </div>
          {isOwner && m.role !== 'owner' && <Btn variant="ghost" disabled={busyId === m.user_id} title={`Remove ${m.email}`}
            onClick={() => { if (window.confirm(`Remove ${m.email} from ${workspace.name}? They’ll lose access immediately.`)) void runAction(m.user_id, () => workspacesAPI.removeMember(workspace.id, m.user_id), refreshMembers, 'Could not remove the member. Please try again.'); }}>
            {busyId === m.user_id ? 'Removing…' : 'Remove'}
          </Btn>}
        </li>;
      })}
    </ul>}

    {isOwner && <>
      <SectionHead title="Pending invites" hint="Invite links aren’t stored, so they can’t be copied again. Create a new link to replace a lost one." />
      {invites.isError && <p role="alert" style={{ color: 'var(--error)', fontSize: 14 }}>{errorMessage(invites.error, 'Could not load invites.')}</p>}
      {invites.data && invites.data.length === 0 && <p style={meta}>No pending invites.</p>}
      {invites.data && invites.data.length > 0 && <ul aria-label="Pending invites" style={{ listStyle: 'none', margin: 0, padding: 0 }}>
        {invites.data.map(inv => <li key={inv.id} style={row}>
          <div style={{ minWidth: 0 }}>
            <div style={{ fontSize: 14.5, fontWeight: 500, overflowWrap: 'anywhere' }}>{inv.email}</div>
            <div style={meta}>Invited {formatDate(inv.created_at)} · expires {formatDate(inv.expires_at)}</div>
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <Btn disabled={inviting || busyId === inv.id} title={`Create a new link for ${inv.email}`} onClick={() => void sendInvite(inv.email)}>New link</Btn>
            <Btn variant="ghost" disabled={busyId === inv.id} title={`Revoke the invite for ${inv.email}`}
              onClick={() => void runAction(inv.id, () => workspacesAPI.revokeInvite(workspace.id, inv.id), refreshInvites, 'Could not revoke the invite. Please try again.')}>
              {busyId === inv.id ? 'Revoking…' : 'Revoke'}
            </Btn>
          </div>
        </li>)}
      </ul>}
    </>}
  </div>;
}

const SLACK_WEBHOOK_DOCS = 'https://api.slack.com/messaging/webhooks';

const TARGET_PLACEHOLDER: Record<AlertChannel['kind'], string> = {
  slack: 'https://hooks.slack.com/services/…',
  webhook: 'https://example.com/tracium-alerts',
  email: 'oncall@example.com, lead@example.com',
};

function AlertsView({ workspace }: { workspace: Workspace }) {
  const { workspacesAPI } = useAPIClient();
  const queryClient = useQueryClient();
  const isOwner = workspace.role.toLowerCase() === 'owner';
  const key = ['alert-channels', workspace.id];
  const channels = useQuery({ queryKey: key, queryFn: () => workspacesAPI.alertChannels(workspace.id) });
  const refresh = () => void queryClient.invalidateQueries({ queryKey: key });

  const [kind, setKind] = useState<AlertChannel['kind']>('slack');
  const [name, setName] = useState('');
  const [target, setTarget] = useState('');
  const [minSeverity, setMinSeverity] = useState<AlertChannel['min_severity']>('warning');
  const [creating, setCreating] = useState(false);
  const [formError, setFormError] = useState('');
  const [secret, setSecret] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [actionError, setActionError] = useState('');
  const [results, setResults] = useState<Record<string, AlertTestResult>>({});

  const backtest = useIncidentBacktest('30d', channels.data?.items.length === 0);
  const emailEnabled = channels.data?.email_enabled ?? false;
  const kinds = [
    { id: 'slack', label: 'Slack' },
    { id: 'webhook', label: 'Webhook' },
    ...(emailEnabled ? [{ id: 'email', label: 'Email' }] : []),
  ];

  const create = async () => {
    setCreating(true);
    setFormError('');
    try {
      const created = await workspacesAPI.createAlertChannel(workspace.id, { kind, name: name.trim(), target: target.trim(), min_severity: minSeverity, notify_resolved: true, enabled: true });
      setSecret(created.secret ?? null);
      setName('');
      setTarget('');
      refresh();
    } catch (err) {
      setFormError(errorMessage(err, 'Could not add the channel. Please try again.'));
    } finally {
      setCreating(false);
    }
  };

  const run = async (id: string, action: () => Promise<void>, fallback: string) => {
    setBusyId(id);
    setActionError('');
    try {
      await action();
    } catch (err) {
      setActionError(errorMessage(err, fallback));
    } finally {
      setBusyId(null);
    }
  };

  const divider = '1px solid color-mix(in srgb, var(--border) 50%, transparent)';
  const row: React.CSSProperties = { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '8px 24px', flexWrap: 'wrap', padding: '14px 0', borderBottom: divider };
  const meta: React.CSSProperties = { fontSize: 13, color: 'var(--muted)' };
  const kindLabel = (k: string) => k === 'slack' ? 'Slack' : k === 'webhook' ? 'Webhook' : 'Email';

  return <div>
    <SectionHead
      first
      title="Alerts"
      hint={isOwner
        ? `Where Tracium sends a message when an incident in ${workspace.name} opens, escalates or resolves.`
        : `Where Tracium sends a message when an incident in ${workspace.name} opens, escalates or resolves. Only the workspace owner can change channels.`}
    />

    {actionError && <p role="alert" style={{ color: 'var(--error)', fontSize: 14 }}>{actionError}</p>}
    {channels.isLoading && <p style={meta}>Loading channels…</p>}
    {channels.isError && <p role="alert" style={{ color: 'var(--error)', fontSize: 14 }}>{errorMessage(channels.error, 'Could not load alert channels.')}</p>}
    {channels.data && channels.data.items.length === 0 && backtest.data && <p style={{ fontSize: 14, margin: '0 0 8px' }}>
      {backtest.data.incidents === 0
        ? 'Nothing would have fired in the last 30 days.'
        : `In the last 30 days Tracium would have alerted you ${backtest.data.incidents} time${backtest.data.incidents === 1 ? '' : 's'}.`}
    </p>}
    {channels.data && channels.data.items.length === 0 && <p style={{ ...meta, fontSize: 14, lineHeight: 1.6, maxWidth: 620 }}>
      No channels yet. Each alert names the workflow or client, lists the signals that crossed their baseline, the release that likely caused it, and links to example runs and the incident.
      For Slack, create an <a href={SLACK_WEBHOOK_DOCS} target="_blank" rel="noreferrer" style={{ color: 'var(--accent)' }}>incoming webhook</a> and paste its URL below.
    </p>}
    {channels.data && channels.data.items.length > 0 && <ul aria-label="Alert channels" style={{ listStyle: 'none', margin: 0, padding: 0 }}>
      {channels.data.items.map(c => {
        const result = results[c.id];
        return <li key={c.id} style={row}>
          <div style={{ minWidth: 0, flex: '1 1 260px' }}>
            <div style={{ fontSize: 14.5, fontWeight: 500, overflowWrap: 'anywhere' }}>{c.name}</div>
            <div style={{ ...meta, overflowWrap: 'anywhere' }}>{kindLabel(c.kind)} · {c.target}</div>
            <div style={meta}>{c.min_severity === 'critical' ? 'Critical only' : 'Warning and critical'}{c.notify_resolved ? ' · resolved too' : ''}</div>
            {result && <div role="status" style={{ fontSize: 13, marginTop: 4, color: result.ok ? 'var(--accent)' : 'var(--error)' }}>
              {result.ok ? 'Test message sent.' : `Test failed: ${result.error ?? 'unknown error'}`}
            </div>}
          </div>
          {isOwner && <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <label style={{ display: 'inline-flex', alignItems: 'center', gap: 8, ...meta }}>
              <Toggle on={c.enabled} label={`${c.enabled ? 'Disable' : 'Enable'} ${c.name}`} onChange={v => void run(c.id, async () => { await workspacesAPI.updateAlertChannel(workspace.id, c.id, { enabled: v }); refresh(); }, 'Could not update the channel.')} />
              {c.enabled ? 'On' : 'Off'}
            </label>
            <Btn disabled={busyId === c.id} title={`Send a test message to ${c.name}`} onClick={() => void run(c.id, async () => {
              const r = await workspacesAPI.testAlertChannel(workspace.id, c.id);
              setResults(prev => ({ ...prev, [c.id]: r }));
            }, 'Could not send the test message.')}>{busyId === c.id ? 'Sending…' : 'Send test'}</Btn>
            <Btn variant="ghost" disabled={busyId === c.id} title={`Delete ${c.name}`}
              onClick={() => { if (window.confirm(`Delete ${c.name}? It stops receiving alerts.`)) void run(c.id, async () => { await workspacesAPI.deleteAlertChannel(workspace.id, c.id); refresh(); }, 'Could not delete the channel.'); }}>Delete</Btn>
          </div>}
        </li>;
      })}
    </ul>}

    {secret && <div role="status" style={{ padding: '14px 16px', margin: '16px 0 8px', borderRadius: 8, border: '1px solid color-mix(in srgb, var(--accent) 35%, transparent)', background: 'color-mix(in srgb, var(--accent) 8%, transparent)' }}>
      <p style={{ fontSize: 14, margin: '0 0 10px', lineHeight: 1.55 }}>
        Signing secret for the webhook. It is shown only once. Verify each request&apos;s <code>X-Tracium-Signature</code>: <code>sha256=</code> HMAC-SHA256 of <code>&lt;X-Tracium-Timestamp&gt;.&lt;body&gt;</code> with this secret.
      </p>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: '8px 16px', flexWrap: 'wrap' }}>
        <code style={{ flex: '1 1 280px', minWidth: 0, overflowWrap: 'anywhere', fontSize: 13, fontFamily: 'var(--font-mono)' }}>{secret}</code>
        <CopyButton key={secret} value={secret} label="Copy secret" />
      </div>
      <div style={{ marginTop: 4 }}><Btn variant="ghost" onClick={() => setSecret(null)}>Done</Btn></div>
    </div>}

    {isOwner && <>
      <SectionHead title="Add channel" />
      <form onSubmit={e => { e.preventDefault(); if (name.trim() && target.trim()) void create(); }} aria-busy={creating}>
        <fieldset disabled={creating} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}>
          <Field label="Kind" hint={kind === 'webhook' ? 'A signed JSON POST, for PagerDuty, Teams or your own service.' : kind === 'email' ? 'Comma-separated addresses.' : 'A Slack incoming webhook URL.'}>
            <Segmented value={kind} onChange={v => setKind(v as AlertChannel['kind'])} options={kinds} />
          </Field>
          <Field label="Name">
            <Input label="Channel name" value={name} onChange={e => setName(e.target.value)} placeholder="#oncall" />
          </Field>
          <Field label={kind === 'email' ? 'Addresses' : 'URL'}>
            <Input label="Channel target" mono value={target} onChange={e => setTarget(e.target.value)} placeholder={TARGET_PLACEHOLDER[kind]} autoComplete="off" />
          </Field>
          <Field label="Minimum severity" hint="Incidents below it are not sent." last
            right={<Btn variant="primary" type="submit" disabled={creating || !name.trim() || !target.trim()}><IconPlus size={13} />{creating ? 'Adding…' : 'Add channel'}</Btn>}>
            <Segmented value={minSeverity} onChange={v => setMinSeverity(v as AlertChannel['min_severity'])} options={[{ id: 'warning', label: 'Warning' }, { id: 'critical', label: 'Critical' }]} />
          </Field>
        </fieldset>
        {formError && <p role="alert" style={{ color: 'var(--error)', fontSize: 14, margin: '0 0 12px' }}>{formError}</p>}
      </form>
      <BudgetRules workspace={workspace} />
    </>}
  </div>;
}

const SCOPE_LABEL: Record<AlertRule['scope'], string> = { workspace: 'Workspace', workflow: 'Workflow', client: 'Client' };

function ruleLabel(r: AlertRule) {
  const who = r.scope === 'workspace' ? 'Workspace' : r.subject === '*' ? `Each ${r.scope}` : `${SCOPE_LABEL[r.scope]} ${r.subject}`;
  return `${who} · $${r.threshold_usd.toFixed(2)} per ${r.period}`;
}

function BudgetRules({ workspace }: { workspace: Workspace }) {
  const { workspacesAPI } = useAPIClient();
  const queryClient = useQueryClient();
  const key = ['alert-rules', workspace.id];
  const rules = useQuery({ queryKey: key, queryFn: () => workspacesAPI.alertRules(workspace.id) });
  const refresh = () => void queryClient.invalidateQueries({ queryKey: key });

  const [scope, setScope] = useState<AlertRule['scope']>('workspace');
  const [subject, setSubject] = useState('');
  const [period, setPeriod] = useState<AlertRule['period']>('day');
  const [limit, setLimit] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const amount = Number(limit);
  const valid = amount > 0 && Number.isFinite(amount) && (scope === 'workspace' || subject.trim() !== '');

  const act = async (action: () => Promise<unknown>, fallback: string) => {
    setBusy(true);
    setError('');
    try {
      await action();
      refresh();
    } catch (err) {
      setError(errorMessage(err, fallback));
    } finally {
      setBusy(false);
    }
  };

  const create = () => act(async () => {
    await workspacesAPI.createAlertRule(workspace.id, { scope, subject: scope === 'workspace' ? '' : subject.trim(), period, threshold_usd: amount, enabled: true });
    setSubject('');
    setLimit('');
  }, 'Could not add the budget rule.');

  const meta: React.CSSProperties = { fontSize: 13, color: 'var(--muted)' };
  return <>
    <SectionHead title="Budget rules" hint="A fixed spend limit per UTC day or month. It warns at 80% and goes critical at 100%, once per period, through the channels above." />
    {error && <p role="alert" style={{ color: 'var(--error)', fontSize: 14 }}>{error}</p>}
    {rules.isError && <p role="alert" style={{ color: 'var(--error)', fontSize: 14 }}>{errorMessage(rules.error, 'Could not load budget rules.')}</p>}
    {rules.data && rules.data.items.length > 0 && <ul aria-label="Budget rules" style={{ listStyle: 'none', margin: '0 0 8px', padding: 0 }}>
      {rules.data.items.map(r => <li key={r.id} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '8px 24px', flexWrap: 'wrap', padding: '12px 0', borderBottom: '1px solid color-mix(in srgb, var(--border) 50%, transparent)' }}>
        <div style={{ fontSize: 14.5, overflowWrap: 'anywhere', minWidth: 0 }}>{ruleLabel(r)}</div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <label style={{ display: 'inline-flex', alignItems: 'center', gap: 8, ...meta }}>
            <Toggle on={r.enabled} label={`${r.enabled ? 'Disable' : 'Enable'} ${ruleLabel(r)}`} onChange={v => void act(() => workspacesAPI.updateAlertRule(workspace.id, r.id, { enabled: v }), 'Could not update the rule.')} />
            {r.enabled ? 'On' : 'Off'}
          </label>
          <Btn variant="ghost" disabled={busy} title={`Delete ${ruleLabel(r)}`}
            onClick={() => { if (window.confirm(`Delete the rule ${ruleLabel(r)}?`)) void act(() => workspacesAPI.deleteAlertRule(workspace.id, r.id), 'Could not delete the rule.'); }}>Delete</Btn>
        </div>
      </li>)}
    </ul>}
    <form onSubmit={e => { e.preventDefault(); if (valid) void create(); }} aria-busy={busy}>
      <fieldset disabled={busy} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}>
        <Field label="Applies to">
          <Segmented value={scope} onChange={v => setScope(v as AlertRule['scope'])} options={[{ id: 'workspace', label: 'Workspace' }, { id: 'workflow', label: 'Workflow' }, { id: 'client', label: 'Client' }]} />
        </Field>
        {scope !== 'workspace' && <Field label={scope === 'workflow' ? 'Workflow' : 'Client'} hint={`A ${scope} name, or * for each ${scope} separately.`}>
          <Input label="Rule subject" mono value={subject} onChange={e => setSubject(e.target.value)} placeholder="*" />
        </Field>}
        <Field label="Period">
          <Segmented value={period} onChange={v => setPeriod(v as AlertRule['period'])} options={[{ id: 'day', label: 'Day' }, { id: 'month', label: 'Month' }]} />
        </Field>
        <Field label="Limit" last
          right={<Btn variant="primary" type="submit" disabled={busy || !valid}><IconPlus size={13} />Add rule</Btn>}>
          <Input label="Limit in USD" type="number" prefix="$" value={limit} onChange={e => setLimit(e.target.value)} placeholder="50" />
        </Field>
      </fieldset>
    </form>
  </>;
}

type DangerVariant = 'secondary' | 'danger';

interface DangerItem {
  title: string;
  hint: string;
  cta: string;
  variant: DangerVariant;
}

const DANGER_ITEMS: DangerItem[] = [
  {
    title: 'Transfer ownership',
    hint: 'Move ownership of this workspace to another member. The current owner becomes an Admin.',
    cta: 'Transfer', variant: 'secondary',
  },
  {
    title: 'Pause ingestion',
    hint: 'Stop accepting new spans. Existing data and dashboards remain readable. SDK calls will return 503.',
    cta: 'Pause workspace', variant: 'secondary',
  },
  {
    title: 'Purge trace history',
    hint: 'Delete every span ingested before today. Aggregate metrics older than today are also dropped.',
    cta: 'Purge', variant: 'danger',
  },
  {
    title: 'Delete workspace',
    hint: 'Permanently remove the workspace, all members, traces, keys, and integrations. This cannot be undone.',
    cta: 'Delete', variant: 'danger',
  },
];

function DangerView() {
  return (
    <div>
      <SectionHead
        first
        title="Danger zone"
        hint="Destructive workspace operations. Most require typing the workspace name to confirm and cannot be undone."
      />
      {DANGER_ITEMS.map((item, i) => (
        <div key={item.title} style={{
          display: 'grid', gridTemplateColumns: '1fr auto',
          gap: 24, alignItems: 'center',
          padding: '20px 0',
          borderBottom: i < DANGER_ITEMS.length - 1 ? '1px solid color-mix(in srgb, var(--border) 50%, transparent)' : 'none',
        }}>
          <div>
            <div style={{ fontSize: 15, fontWeight: 500, color: item.variant === 'danger' ? 'var(--error)' : 'var(--foreground)', marginBottom: 4 }}>{item.title}</div>
            <div style={{ fontSize: 13.5, color: 'var(--muted)', lineHeight: 1.55, maxWidth: 580 }}>{item.hint}</div>
          </div>
          <Btn variant={item.variant}>{item.cta}</Btn>
        </div>
      ))}
    </div>
  );
}

export default function SettingsPage({
  sections = [],
  createWorkspace,
  onOpenOverview,
  onOpenApiKeys,
  ingestEndpoint,
  onCancelCreate,
  createMode = false,
  initialTab,
  demo = false,
  workspace = null,
  account = null,
  onSessionRenewed,
}: SettingsPageProps): React.JSX.Element {
  const [tab, setTab] = useState<TabId>(createMode ? 'workspace' : initialTab ?? (!demo ? 'workspace' : 'account'));
  const [creating, setCreating] = useState(createMode);
  const [createdId, setCreatedId] = useState<string | null>(null);
  const [prevCreateMode, setPrevCreateMode] = useState(createMode);
  if (createMode !== prevCreateMode) {
    setPrevCreateMode(createMode);
    if (createMode) {
      setTab('workspace');
      setCreating(true);
      setCreatedId(null);
    }
  }
  // Below the tablet breakpoint the tab rail moves above the content instead of
  // sitting in a fixed 220px left column.
  const stackRail = useMaxWidth(BREAKPOINTS.tablet);

  const selectTab = (t: TabId) => {
    setCreating(false);
    onCancelCreate?.();
    setTab(t);
  };

  // The auth-page preview (demo) shows the rich seeded data; a real signed-in
  // workspace shows its own identity.
  const user: SettingsUser = demo
    ? SET_USER
    : {
        name: account?.name ?? 'Account',
        email: account?.email ?? '',
        initials: account?.initials ?? '—',
        role: workspace?.role ?? 'Owner',
        joined: '—',
      };

  const ws: SettingsWorkspace = demo
    ? SET_WORKSPACE
    : {
        name: workspace?.name ?? '',
        id: workspace?.id ?? '',
        defaultRetention: '30 days',
        members: workspace?.members ?? 1,
      };

  const viewMap: Partial<Record<TabId, React.ReactNode>> = {
    account:   <AccountView user={user} demo={demo} onSessionRenewed={onSessionRenewed} />,
    workspace: creating && createWorkspace
      ? <CreateWorkspaceView onCreate={async draft => {
          const created = await createWorkspace(draft);
          setCreatedId(created.id);
          setCreating(false);
          setTab('workspace');
        }} onCancel={() => { setCreating(false); onCancelCreate?.(); }} />
      : <>
          <WorkspaceView key={ws.id} ws={ws} demo={demo} justCreated={createdId === ws.id} onOpenOverview={onOpenOverview} onOpenApiKeys={onOpenApiKeys} ingestEndpoint={ingestEndpoint} />
          {!demo && createWorkspace && <div style={{ marginTop: 24 }}><Btn onClick={() => { setCreatedId(null); setCreating(true); }}>{ws.id ? 'Create another workspace' : 'Create workspace'}</Btn></div>}
        </>,
    members:   workspace
      ? <MembersView key={workspace.id} workspace={workspace} account={account} />
      : <SectionHead first title="Members" hint="Create a workspace to invite people to it." />,
    alerts:    workspace
      ? <AlertsView key={workspace.id} workspace={workspace} />
      : <SectionHead first title="Alerts" hint="Create a workspace to send its incidents to Slack, a webhook or email." />,
    danger:    <DangerView />,
  };

  return (
    <div style={{ padding: 'clamp(24px, 4vw, 40px) clamp(16px, 4vw, 28px) 96px', maxWidth: 1280, margin: '0 auto' }} data-screen-label="Settings">
      <div style={{ marginBottom: 36 }}>
        <h1 style={{ fontSize: 26, fontWeight: 600, color: 'var(--foreground)', margin: 0, letterSpacing: '-0.02em', lineHeight: 1.15 }}>Settings</h1>
        <p style={{ fontSize: 14, color: 'var(--muted)', margin: '6px 0 0', maxWidth: 620, lineHeight: 1.55 }}>
          Account and workspace settings for <span style={{ color: 'var(--foreground)', fontWeight: 500 }}>{ws.name || 'this workspace'}</span>.
          Select a workspace to view its connection instructions.
        </p>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: stackRail ? '1fr' : '220px 1fr', gap: stackRail ? 24 : 48, alignItems: 'start' }}>
        <TabRail sections={sections} tab={tab} setTab={selectTab} horizontal={stackRail} demo={demo} />
        <div style={{ minWidth: 0 }}>{sections.some(s => s.id === tab) ? React.createElement(sections.find(s => s.id === tab)!.component, {workspace}) : viewMap[tab]}</div>
      </div>
    </div>
  );
}
