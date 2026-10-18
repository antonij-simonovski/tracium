import React from 'react';
import { SEVERITY_META } from '../utils/severity';
import type { SetupIssue } from '../interfaces';

interface SetupIssueNoteProps {
  issue: SetupIssue;
  children?: React.ReactNode;
}

export function SetupIssueNote({ issue, children }: SetupIssueNoteProps) {
  const meta = SEVERITY_META[issue.severity];
  return (
    <div
      style={{
        padding: '10px 12px',
        borderRadius: 8,
        background: meta.tint,
        border: `1px solid ${meta.border}`,
        fontSize: 13.5,
        lineHeight: 1.5,
        color: 'var(--foreground)',
      }}
    >
      {issue.message}
      {children && (
        <div style={{ display: 'flex', alignItems: 'center', gap: 14, flexWrap: 'wrap', marginTop: 6, fontSize: 12.5, color: 'var(--muted)' }}>
          {children}
        </div>
      )}
    </div>
  );
}
