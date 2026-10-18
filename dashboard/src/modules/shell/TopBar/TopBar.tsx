import React from 'react';
import { IconSearch, IconMenu } from '../../../common';
import hover from '../../../common/styles/hover.module.css';
import type { BreadcrumbItem } from '../interfaces';

const RANGES = ['24h', '7d', '30d', '90d', '1y'];

interface TopBarProps {
  breadcrumb: BreadcrumbItem[];
  range: string;
  setRange: (r: string) => void;
  onOpenCmd: () => void;
  embedded?: boolean;
  showRange?: boolean;
  /** Shows the hamburger menu button (mobile: opens the sidebar drawer). */
  showMenu?: boolean;
  /** Opens the sidebar drawer. */
  onOpenNav?: () => void;
}

export function TopBar({
  breadcrumb,
  range,
  setRange,
  onOpenCmd,
  embedded = false,
  showRange = false,
  showMenu = false,
  onOpenNav,
}: TopBarProps): React.ReactElement {
  const isMac = /Mac|iPhone|iPad/.test(navigator.platform);
  return (
    <header
      style={{
        height: 52,
        display: 'flex',
        alignItems: 'center',
        padding: '0 clamp(12px, 3vw, 20px)',
        gap: 12,
        background: 'var(--background)',
        borderBottom: '1px solid var(--border)',
        flexShrink: 0,
        position: embedded ? 'relative' : 'sticky',
        top: 0,
        zIndex: 40,
      }}
    >
      {showMenu && (
        <button
          onClick={onOpenNav}
          aria-label="Open menu"
          className={hover.text}
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: 32,
            height: 32,
            marginLeft: -6,
            flexShrink: 0,
            background: 'transparent',
            border: 'none',
            borderRadius: 7,
            cursor: 'pointer',
          }}
        >
          <IconMenu size={18} />
        </button>
      )}

      <div
        style={{
          flex: 1,
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          minWidth: 0,
        }}
      >
        {breadcrumb.map((crumb, i) => (
          <React.Fragment key={i}>
            {i > 0 && (
              <span style={{ color: 'var(--border-strong)', fontSize: 14 }}>/</span>
            )}
            {crumb.onClick ? (
              <button
                onClick={crumb.onClick}
                onMouseEnter={(e) =>
                  (e.currentTarget.style.color = 'var(--foreground)')
                }
                onMouseLeave={(e) =>
                  (e.currentTarget.style.color =
                    i === breadcrumb.length - 1 ? 'var(--foreground)' : 'var(--muted)')
                }
                style={{
                  fontSize: 14,
                  fontWeight: i === breadcrumb.length - 1 ? 500 : 400,
                  color:
                    i === breadcrumb.length - 1
                      ? 'var(--foreground)'
                      : 'var(--muted)',
                  background: 'transparent',
                  border: 'none',
                  padding: '2px 4px',
                  borderRadius: 4,
                  cursor: 'pointer',
                }}
              >
                {crumb.label}
              </button>
            ) : (
              <span
                style={{
                  fontSize: 14,
                  fontWeight: i === breadcrumb.length - 1 ? 500 : 400,
                  color:
                    i === breadcrumb.length - 1
                      ? 'var(--foreground)'
                      : 'var(--muted)',
                  padding: '2px 4px',
                }}
              >
                {crumb.label}
              </span>
            )}
          </React.Fragment>
        ))}
      </div>

      {showRange && (
        <div
          style={{
            display: 'flex',
            background: 'var(--surface-alt)',
            border: '1px solid var(--border)',
            borderRadius: 7,
            padding: 2,
          }}
        >
          {RANGES.map((r) => (
            <button
              key={r}
              onClick={() => setRange(r)}
              aria-pressed={range === r}
              style={{
                padding: '4px 9px',
                fontSize: 13,
                fontWeight: 500,
                background: range === r ? 'var(--surface)' : 'transparent',
                color: range === r ? 'var(--foreground)' : 'var(--muted)',
                border:
                  '1px solid ' + (range === r ? 'var(--border)' : 'transparent'),
                borderRadius: 5,
                cursor: 'pointer',
              }}
            >
              {r}
            </button>
          ))}
        </div>
      )}

      <button
        onClick={onOpenCmd}
        className={hover.outlined}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          padding: '5px 10px',
          background: 'var(--surface-alt)',
          borderWidth: 1,
          borderStyle: 'solid',
          borderRadius: 7,
          cursor: 'pointer',
          fontSize: 13,
        }}
      >
        <IconSearch size={13} />
        <span>Search</span>
        <kbd
          style={{
            fontSize: 11,
            padding: '1px 5px',
            borderRadius: 4,
            background: 'var(--surface)',
            border: '1px solid var(--border)',
            color: 'var(--muted)',
            fontFamily: 'inherit',
          }}
        >
          {isMac ? '⌘K' : 'Ctrl K'}
        </kbd>
      </button>
    </header>
  );
}
