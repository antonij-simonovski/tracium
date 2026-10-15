import React, { useCallback, useState, useRef } from 'react';
import { IconChevron, IconCheck, IconPlus, IconTrash, handleDialogKeyDown, useClickOutside } from '../../../common';
import hover from '../../../common/styles/hover.module.css';
import type { Workspace } from '../interfaces';

interface WorkspaceSwitcherProps {
  workspace: Workspace | null;
  workspaces: Workspace[];
  setWorkspace: (ws: Workspace) => void;
  createWorkspace: () => void;
  deleteWorkspace: (id: string) => Promise<void>;
}

const ENV_COLOR: Record<Workspace['env'], string> = {
  production: 'var(--accent)',
  development: 'var(--info)',
  staging: 'var(--warning)',
};

const ENV_LABEL: Record<Workspace['env'], string> = {
  production: 'prod',
  development: 'dev',
  staging: 'stg',
};

export function WorkspaceSwitcher({
  workspace,
  workspaces,
  setWorkspace,
  createWorkspace,
  deleteWorkspace,
}: WorkspaceSwitcherProps): React.ReactElement {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);

  const dismiss = useCallback(() => setOpen(false), []);
  useClickOutside(ref, open, dismiss);

  const envColor = workspace ? ENV_COLOR[workspace.env] : 'var(--muted)';
  const envLabel = workspace ? ENV_LABEL[workspace.env] : '';

  const close = () => {
    setOpen(false);
    triggerRef.current?.focus();
  };

  return (
    <div ref={ref} style={{ position: 'relative' }}>
      <button
        ref={triggerRef}
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        onMouseEnter={(e) => {
          if (!open) e.currentTarget.style.background = 'var(--surface-alt)';
        }}
        onMouseLeave={(e) => {
          if (!open) e.currentTarget.style.background = 'transparent';
        }}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          width: '100%',
          padding: '8px 10px',
          background: open ? 'var(--surface-active)' : 'transparent',
          border: '1px solid ' + (open ? 'var(--border-strong)' : 'transparent'),
          borderRadius: 8,
          cursor: 'pointer',
          transition: 'all .14s',
        }}
      >
        <div style={{ flex: 1, minWidth: 0, textAlign: 'left' }}>
          <div
            style={{
              fontSize: 14,
              fontWeight: 600,
              color: 'var(--foreground)',
              lineHeight: 1.2,
              whiteSpace: 'nowrap',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
            }}
          >
            {workspace ? workspace.name : 'No workspace'}
          </div>
          <div
            style={{
              fontSize: 12,
              color: 'var(--muted)',
              display: 'flex',
              alignItems: 'center',
              gap: 4,
              marginTop: 1,
            }}
          >
            {workspace ? (
              <>
                <span
                  style={{
                    padding: '1px 5px',
                    borderRadius: 3,
                    fontSize: 10.5,
                    fontWeight: 600,
                    letterSpacing: '0.08em',
                    background: `color-mix(in srgb, ${envColor} 14%, transparent)`,
                    color: envColor,
                    border: `1px solid color-mix(in srgb, ${envColor} 25%, transparent)`,
                  }}
                >
                  {envLabel}
                </span>
              </>
            ) : (
              <span>Create one to get started</span>
            )}
          </div>
        </div>
        <IconChevron
          size={12}
          style={{
            color: 'var(--muted)',
            flexShrink: 0,
            transform: open ? 'rotate(180deg)' : 'none',
            transition: 'transform .14s',
          }}
        />
      </button>

      {open && (
        <div
          role="dialog"
          aria-modal="true"
          aria-label="Workspaces"
          onKeyDown={(e) => handleDialogKeyDown(e, close)}
          style={{
            position: 'absolute',
            top: 'calc(100% + 6px)',
            left: 0,
            right: 0,
            zIndex: 60,
            background: 'var(--surface)',
            border: '1px solid var(--border-strong)',
            borderRadius: 10,
            boxShadow: '0 12px 40px var(--shadow-popover)',
            overflow: 'hidden',
          }}
        >
          <div
            style={{
              padding: '8px 10px 6px',
              fontSize: 11,
              fontWeight: 600,
              color: 'var(--muted)',
              textTransform: 'uppercase',
              letterSpacing: '0.1em',
            }}
          >
            Workspaces
          </div>
          {workspaces.length === 0 && (
            <div style={{ padding: '4px 10px 8px', fontSize: 13, color: 'var(--muted)' }}>
              No workspaces yet.
            </div>
          )}
          {workspaces.map((ws) => {
            const active = workspace !== null && ws.id === workspace.id;
            const wsEnvColor = ENV_COLOR[ws.env];
            return (
              <div
                key={ws.id}
                onMouseEnter={(e) => {
                  if (!active) e.currentTarget.style.background = 'var(--surface-alt)';
                }}
                onMouseLeave={(e) => {
                  e.currentTarget.style.background = active
                    ? 'var(--surface-active)'
                    : 'transparent';
                }}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  paddingRight: 10,
                  background: active ? 'var(--surface-active)' : 'transparent',
                }}
              >
                <button
                  autoFocus={active}
                  aria-current={active || undefined}
                  onClick={() => {
                    setWorkspace(ws);
                    close();
                  }}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 10,
                    flex: 1,
                    minWidth: 0,
                    padding: '8px 10px',
                    background: 'transparent',
                    border: 'none',
                    cursor: 'pointer',
                    textAlign: 'left',
                    fontFamily: 'inherit',
                  }}
                >
                  <div
                    style={{
                      width: 22,
                      height: 22,
                      borderRadius: 5,
                      background: `linear-gradient(135deg, ${wsEnvColor}, color-mix(in srgb, ${wsEnvColor} 60%, var(--surface)))`,
                      display: 'grid',
                      placeItems: 'center',
                      color:
                        ws.env === 'development' ? '#000' : 'var(--accent-contrast)',
                      fontSize: 9.5,
                      fontWeight: 700,
                      flexShrink: 0,
                    }}
                  >
                    {ws.name.slice(0, 2).toUpperCase()}
                  </div>
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div
                      style={{
                        fontSize: 13.5,
                        fontWeight: 500,
                        color: 'var(--foreground)',
                      }}
                    >
                      {ws.name}
                    </div>
                    <div style={{ fontSize: 12, color: 'var(--muted)' }}>
                      {ws.role} · {ws.members} {ws.members === 1 ? 'member' : 'members'}
                    </div>
                  </div>
                  {active && (
                    <IconCheck size={13} style={{ color: 'var(--accent)', flexShrink: 0 }} />
                  )}
                </button>
                {ws.role.toLowerCase() === 'owner' && <button
                  aria-label={`Delete ${ws.name} workspace`}
                  title={`Delete ${ws.name}`}
                  onClick={() => {
                    if (!window.confirm(`Delete ${ws.name}? This can’t be undone.`)) return;
                    deleteWorkspace(ws.id).catch(() => window.alert(`Could not delete ${ws.name}. Please try again.`));
                  }}
                  className={hover.danger}
                  style={{
                    display: 'grid',
                    placeItems: 'center',
                    flexShrink: 0,
                    width: 24,
                    height: 24,
                    padding: 0,
                    background: 'transparent',
                    border: 'none',
                    borderRadius: 5,
                    cursor: 'pointer',
                  }}
                >
                  <IconTrash size={13} />
                </button>}
              </div>
            );
          })}
          <div
            style={{
              borderTop: '1px solid var(--border)',
              padding: '6px 10px 8px',
            }}
          >
            <button
              autoFocus={workspaces.length === 0}
              onClick={() => {
                createWorkspace();
                setOpen(false);
              }}
              className={hover.text}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 7,
                width: '100%',
                padding: '6px 8px',
                borderRadius: 6,
                background: 'transparent',
                border: 'none',
                cursor: 'pointer',
                fontSize: 13,
              }}
            >
              <IconPlus size={12} />
              Create workspace
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
