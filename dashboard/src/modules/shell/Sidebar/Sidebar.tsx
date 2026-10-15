import React, { useEffect, useRef, useState } from 'react';
import {
  IconHome,
  IconWorkflows,
  IconUsers,
  IconUsage,
  IconKey,
  IconSettings,
  IconLogout,
  IconX,
  IconSun,
  IconMoon,
  IconPanel,
  IconStar,
  handleDialogKeyDown,
} from '../../../common';
import hover from '../../../common/styles/hover.module.css';
import type { Workspace } from '../interfaces';
import { WorkspaceSwitcher } from '../WorkspaceSwitcher';
import { TraciumWordmark } from '../Logo';

interface NavItemData {
  id: string;
  icon: React.ReactNode;
  label: string;
}

interface NavItemProps {
  item: NavItemData;
  active: boolean;
  onClick: () => void;
  collapsed?: boolean;
}

interface SidebarProps {
  items?: readonly { id: string; label: string; icon?: React.ReactNode }[];
  currentView: string;
  setView: (v: string) => void;
  workspace: Workspace | null;
  workspaces: Workspace[];
  setWorkspace: (ws: Workspace) => void;
  createWorkspace: () => void;
  deleteWorkspace: (id: string) => Promise<void>;
  embedded?: boolean;
  /** Logs the account out. Hidden when absent (e.g. the embedded preview). */
  onLogout?: () => void;
  /** When true, the sidebar renders as an off-canvas drawer (small screens). */
  mobile?: boolean;
  /** Whether the drawer is currently open (only relevant when mobile). */
  open?: boolean;
  /** Closes the drawer — called on backdrop click and after navigation. */
  onClose?: () => void;
}

const NAV_ITEMS: NavItemData[] = [
  { id: 'overview', icon: <IconHome size={16} />, label: 'Overview' },
  { id: 'workflows', icon: <IconWorkflows size={16} />, label: 'Workflows' },
  { id: 'usage', icon: <IconUsage size={16} />, label: 'Usage' },
  { id: 'users', icon: <IconUsers size={16} />, label: 'Clients' },
  { id: 'keys', icon: <IconKey size={16} />, label: 'API Keys' },
];

function NavItem({ item, active, onClick, collapsed }: NavItemProps): React.ReactElement {
  return (
    <button
      onClick={onClick}
      title={collapsed ? item.label : undefined}
      aria-label={collapsed ? item.label : undefined}
      onMouseEnter={(e) => {
        if (!active) {
          e.currentTarget.style.background = 'var(--surface-alt)';
          e.currentTarget.style.color = 'var(--foreground)';
        }
      }}
      onMouseLeave={(e) => {
        if (!active) {
          e.currentTarget.style.background = 'transparent';
          e.currentTarget.style.color = 'var(--muted)';
        }
      }}
      style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: collapsed ? 'center' : undefined,
        gap: 10,
        width: '100%',
        padding: '7px 10px',
        borderRadius: 7,
        background: active ? 'var(--surface-active)' : 'transparent',
        border: '1px solid ' + (active ? 'var(--border)' : 'transparent'),
        color: active ? 'var(--foreground)' : 'var(--muted)',
        fontSize: 14,
        fontWeight: active ? 500 : 400,
        cursor: 'pointer',
        textAlign: 'left',
        transition: 'all .12s',
      }}
    >
      <span style={{ flexShrink: 0, opacity: active ? 1 : 0.65 }}>{item.icon}</span>
      {!collapsed && item.label}
    </button>
  );
}

export function Sidebar({
  items = [],
  currentView,
  setView,
  workspace,
  workspaces,
  setWorkspace,
  createWorkspace,
  deleteWorkspace,
  embedded = false,
  onLogout,
  mobile = false,
  open = false,
  onClose,
}: SidebarProps): React.ReactElement {
  const closeRef = useRef<HTMLButtonElement>(null);
  const [theme, setTheme] = useState(document.documentElement.dataset.theme);
  const toggleTheme = () => {
    const next = theme === 'light' ? 'dark' : 'light';
    document.documentElement.dataset.theme = next;
    localStorage.setItem('tracium_theme', next);
    setTheme(next);
  };
  const [collapsedPref, setCollapsedPref] = useState(localStorage.getItem('tracium_sidebar') === 'collapsed');
  const collapsed = collapsedPref && !mobile;
  const toggleCollapsed = () => {
    localStorage.setItem('tracium_sidebar', collapsedPref ? 'expanded' : 'collapsed');
    setCollapsedPref(!collapsedPref);
  };
  const drawerOpen = mobile && open;
  useEffect(() => {
    if (drawerOpen) closeRef.current?.focus();
  }, [drawerOpen]);

  const navigate = (v: string) => {
    setView(v);
    if (mobile) onClose?.();
  };

  const positioning: React.CSSProperties = mobile
    ? {
        position: 'fixed',
        top: 0,
        left: 0,
        bottom: 0,
        height: '100dvh',
        zIndex: 60,
        transform: open ? 'translateX(0)' : 'translateX(-100%)',
        visibility: open ? 'visible' : 'hidden',
        transition: 'transform .22s ease, visibility .22s',
        boxShadow: open ? '0 0 40px rgba(0,0,0,0.5)' : 'none',
      }
    : embedded
      ? { height: '100%', minHeight: 0 }
      : { minHeight: '100vh', position: 'sticky', top: 0, height: '100vh' };

  return (
    <>
      {mobile && (
        <div
          onClick={onClose}
          aria-hidden
          style={{
            position: 'fixed',
            inset: 0,
            zIndex: 55,
            background: 'rgba(0,0,0,0.5)',
            opacity: open ? 1 : 0,
            pointerEvents: open ? 'auto' : 'none',
            transition: 'opacity .22s ease',
          }}
        />
      )}
    <aside
      {...(drawerOpen && { role: 'dialog', 'aria-modal': true, 'aria-label': 'Navigation' })}
      onKeyDown={drawerOpen ? (e) => handleDialogKeyDown(e, () => onClose?.()) : undefined}
      style={{
        width: collapsed ? 64 : 240,
        maxWidth: '85vw',
        flexShrink: 0,
        background: 'var(--sidebar-bg)',
        borderRight: '1px solid var(--border)',
        display: 'flex',
        flexDirection: 'column',
        overflowY: 'auto',
        transition: 'width .18s ease',
        ...positioning,
      }}
    >
      {mobile && (
        <button
          ref={closeRef}
          onClick={onClose}
          aria-label="Close menu"
          style={{
            position: 'absolute',
            top: 14,
            right: 12,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: 28,
            height: 28,
            background: 'transparent',
            border: 'none',
            borderRadius: 6,
            color: 'var(--muted)',
            cursor: 'pointer',
          }}
        >
          <IconX size={16} />
        </button>
      )}
      <div style={{ padding: '16px 12px 10px' }}>
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: collapsed ? 'center' : 'space-between',
            gap: 10,
            minHeight: 28,
            paddingBottom: 14,
            borderBottom: '1px solid var(--border)',
          }}
        >
          {!collapsed && <TraciumWordmark height={22} />}
          {!mobile && (
            <button
              onClick={toggleCollapsed}
              aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
              title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
              aria-expanded={!collapsed}
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                width: 28,
                height: 28,
                background: 'transparent',
                border: 'none',
                borderRadius: 6,
                color: 'var(--muted)',
                cursor: 'pointer',
              }}
            >
              <IconPanel size={16} />
            </button>
          )}
        </div>

        {!collapsed && (
          <div style={{ paddingTop: 10 }}>
            <WorkspaceSwitcher
              workspace={workspace}
              workspaces={workspaces}
              setWorkspace={setWorkspace}
              createWorkspace={createWorkspace}
              deleteWorkspace={deleteWorkspace}
            />
          </div>
        )}
      </div>

      <nav
        style={{
          flex: 1,
          padding: '4px 12px',
          display: 'flex',
          flexDirection: 'column',
          gap: 1,
        }}
      >
        {[...NAV_ITEMS, ...items.map(item => ({ ...item, icon: item.icon ?? null }))].map((item) => (
          <NavItem
            key={item.id}
            item={item}
            active={
              currentView === item.id ||
              ((currentView === 'trace' || currentView === 'compare') && item.id === 'workflows')
            }
            onClick={() => navigate(item.id)}
            collapsed={collapsed}
          />
        ))}
      </nav>

      <div style={{ padding: '12px', borderTop: '1px solid var(--border)' }}>
        <a
          href="https://github.com/antonij-tracium/tracium"
          target="_blank"
          rel="noopener noreferrer"
          aria-label="Star on GitHub"
          title={collapsed ? 'Star on GitHub' : undefined}
          className={collapsed ? hover.text : undefined}
          style={{ display: 'flex', justifyContent: collapsed ? 'center' : undefined, padding: '4px 10px 10px' }}
        >
          {collapsed
            ? <IconStar size={16} />
            : <img src="https://img.shields.io/github/stars/antonij-tracium/tracium?label=Star&style=social" alt="" height={20} />}
        </a>
        <NavItem
          item={theme === 'light'
            ? { id: 'theme', icon: <IconMoon size={16} />, label: 'Dark mode' }
            : { id: 'theme', icon: <IconSun size={16} />, label: 'Light mode' }}
          active={false}
          onClick={toggleTheme}
          collapsed={collapsed}
        />
        <NavItem
          item={{ id: 'settings', icon: <IconSettings size={16} />, label: 'Settings' }}
          active={currentView === 'settings'}
          onClick={() => navigate('settings')}
          collapsed={collapsed}
        />
        {onLogout && (
          <button
            onClick={onLogout}
            title={collapsed ? 'Log out' : undefined}
            aria-label={collapsed ? 'Log out' : undefined}
            className={hover.danger}
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: collapsed ? 'center' : undefined,
              gap: 10,
              width: '100%',
              marginTop: 8,
              padding: '7px 10px',
              background: 'transparent',
              border: 'none',
              borderRadius: 6,
              cursor: 'pointer',
              fontSize: 13.5,
              fontWeight: 500,
              fontFamily: 'inherit',
              textAlign: 'left',
            }}
          >
            <IconLogout size={15} />
            {!collapsed && 'Log out'}
          </button>
        )}
      </div>
    </aside>
    </>
  );
}
