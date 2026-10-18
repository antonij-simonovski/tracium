import type { ReactNode } from 'react';

export function Centered({ minHeight = 320, children }: { minHeight?: number; children: ReactNode }) {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        minHeight,
        color: 'var(--muted)',
        fontSize: 14,
      }}
    >
      {children}
    </div>
  );
}
