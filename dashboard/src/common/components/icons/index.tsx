import React from 'react';

export interface IconProps {
  d: string | React.ReactNode;
  size?: number;
  stroke?: number;
  style?: React.CSSProperties;
}

export function Icon({ d, size = 16, stroke = 1.5, style }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={stroke}
      strokeLinecap="round"
      strokeLinejoin="round"
      style={style}
    >
      {typeof d === 'string' ? <path d={d} /> : d}
    </svg>
  );
}

export type IconOnlyProps = Omit<IconProps, 'd'>;

export const IconHome         = (p: IconOnlyProps) => <Icon d="M3 10.5L12 3l9 7.5V20a1 1 0 01-1 1h-5v-7h-6v7H4a1 1 0 01-1-1v-9.5z" {...p} />;
export const IconWorkflows       = (p: IconOnlyProps) => <Icon d="M4 6h7v7H4zM13 11h7v7h-7zM13 4h7M4 17h7" {...p} />;
export const IconUsers      = (p: IconOnlyProps) => <Icon d="M3 21V8l6-4 6 4v13M9 21v-6h0M15 14h6v7H3M18 18h0M18 14v0" {...p} />;
export const IconUsage        = (p: IconOnlyProps) => <Icon d="M4 20V10M10 20V4M16 20v-6M22 20v-9" {...p} />;
export const IconKey          = (p: IconOnlyProps) => <Icon d="M15 7a4 4 0 11-6.93 3.93L3 16v4h4v-3h3v-3l.07-.07A4 4 0 0115 7z" {...p} />;
export const IconSettings     = (p: IconOnlyProps) => <Icon d="M12 15a3 3 0 100-6 3 3 0 000 6zM19.4 12c0-.4 0-.8-.1-1.2l2-1.5-2-3.5-2.3.9c-.6-.5-1.3-.9-2-1.2L14.6 3h-4l-.4 2.5c-.7.3-1.4.7-2 1.2L5.9 5.8l-2 3.5 2 1.5c-.1.4-.1.8-.1 1.2s0 .8.1 1.2l-2 1.5 2 3.5 2.3-.9c.6.5 1.3.9 2 1.2L9.4 21h4l.4-2.5c.7-.3 1.4-.7 2-1.2l2.3.9 2-3.5-2-1.5c.1-.4.1-.8.1-1.2z" {...p} />;
export const IconSearch       = (p: IconOnlyProps) => <Icon d="M11 4a7 7 0 015.2 11.8L21 20M13 18a7 7 0 01-7-7" {...p} />;
export const IconChevron      = (p: IconOnlyProps) => <Icon d="M6 9l6 6 6-6" {...p} />;
export const IconChevronRight = (p: IconOnlyProps) => <Icon d="M9 6l6 6-6 6" {...p} />;
export const IconArrowRight   = (p: IconOnlyProps) => <Icon d="M5 12h14M13 5l7 7-7 7" {...p} />;
export const IconArrowUp      = (p: IconOnlyProps) => <Icon d="M7 14l5-5 5 5" {...p} />;
export const IconArrowDown    = (p: IconOnlyProps) => <Icon d="M7 10l5 5 5-5" {...p} />;
export const IconAlert        = (p: IconOnlyProps) => <Icon d="M12 9v4M12 17h.01M4.5 19h15a2 2 0 001.7-3L13.7 5a2 2 0 00-3.4 0L2.8 16a2 2 0 001.7 3z" {...p} />;
export const IconCheck        = (p: IconOnlyProps) => <Icon d="M5 12l5 5 9-11" {...p} />;
export const IconX            = (p: IconOnlyProps) => <Icon d="M6 6l12 12M18 6L6 18" {...p} />;
export const IconPlus         = (p: IconOnlyProps) => <Icon d="M12 5v14M5 12h14" {...p} />;
export const IconCopy         = (p: IconOnlyProps) => <Icon d="M9 4h9a2 2 0 012 2v9M5 8h9a2 2 0 012 2v9a2 2 0 01-2 2H5a2 2 0 01-2-2v-9a2 2 0 012-2z" {...p} />;
export const IconTrash        = (p: IconOnlyProps) => <Icon d="M4 7h16M9 7V5a2 2 0 012-2h2a2 2 0 012 2v2M6 7l1 13a2 2 0 002 2h6a2 2 0 002-2l1-13" {...p} />;
export const IconBuilding     = (p: IconOnlyProps) => <Icon d="M4 21V5a2 2 0 012-2h8a2 2 0 012 2v16M4 21h16M9 7h2M9 11h2M9 15h2M16 11h2v10" {...p} />;
export const IconMore         = (p: IconOnlyProps) => <Icon d="M5 12h.01M12 12h.01M19 12h.01" stroke={2.5} {...p} />;
export const IconUser         = (p: IconOnlyProps) => <Icon d="M20 21v-2a4 4 0 00-4-4H8a4 4 0 00-4 4v2M12 11a4 4 0 100-8 4 4 0 000 8z" {...p} />;
export const IconLogout       = (p: IconOnlyProps) => <Icon d="M15 3h4a1 1 0 011 1v16a1 1 0 01-1 1h-4M10 17l5-5-5-5M15 12H3" {...p} />;
export const IconMenu         = (p: IconOnlyProps) => <Icon d="M4 6h16M4 12h16M4 18h16" {...p} />;
export const IconSun          = (p: IconOnlyProps) => <Icon d="M12 16a4 4 0 100-8 4 4 0 000 8zM12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" {...p} />;
export const IconPanel        = (p: IconOnlyProps) => <Icon d="M5 4h14a1 1 0 011 1v14a1 1 0 01-1 1H5a1 1 0 01-1-1V5a1 1 0 011-1zM9 4v16" {...p} />;
export const IconGithub       = (p: IconOnlyProps) => <Icon d={<path fill="currentColor" stroke="none" d="M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.1.79-.25.79-.56v-1.97c-3.2.7-3.87-1.54-3.87-1.54-.52-1.33-1.28-1.69-1.28-1.69-1.04-.71.08-.7.08-.7 1.15.08 1.76 1.18 1.76 1.18 1.03 1.76 2.69 1.25 3.35.96.1-.74.4-1.25.73-1.54-2.55-.29-5.24-1.28-5.24-5.68 0-1.26.45-2.28 1.18-3.09-.12-.29-.51-1.46.11-3.05 0 0 .97-.31 3.17 1.18a11 11 0 015.77 0c2.2-1.49 3.17-1.18 3.17-1.18.63 1.59.23 2.76.11 3.05.74.81 1.18 1.83 1.18 3.09 0 4.41-2.69 5.38-5.25 5.67.41.36.78 1.06.78 2.14v3.17c0 .31.21.67.8.56A11.5 11.5 0 0023.5 12C23.5 5.65 18.35.5 12 .5z" />} {...p} />;
export const IconMoon         = (p: IconOnlyProps) => <Icon d="M21 12.8A9 9 0 1111.2 3a7 7 0 009.8 9.8z" {...p} />;

export function IconDot({ size = 6, color = 'currentColor' }: { size?: number; color?: string }) {
  return (
    <span
      style={{
        display: 'inline-block',
        width: size,
        height: size,
        borderRadius: 999,
        background: color,
      }}
    />
  );
}
