import React from 'react';
import { useMaxWidth, BREAKPOINTS } from '../../../common';

interface OverviewLayoutProps {
  masthead: React.ReactNode;
  setupChecks?: React.ReactNode;
  kpis: React.ReactNode;
  outlierChips?: React.ReactNode;
  charts: React.ReactNode;
  outliers?: React.ReactNode;
  failures: React.ReactNode;
  feed: React.ReactNode;
  workflows: React.ReactNode;
}

export function OverviewLayout({
  masthead,
  setupChecks,
  kpis,
  outlierChips,
  charts,
  outliers,
  failures,
  feed,
  workflows,
}: OverviewLayoutProps) {
  const stacked = useMaxWidth(BREAKPOINTS.tablet);

  return (
    <div style={{ padding: 'clamp(20px, 4vw, 32px) clamp(16px, 4vw, 40px) 64px', maxWidth: 1480, margin: '0 auto' }}>
      {masthead}
      {setupChecks}
      {kpis}
      {outlierChips}
      {charts}
      {outliers}
      {failures}
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: stacked
            ? '1fr'
            : '1.15fr 1fr',
          gap: stacked ? 32 : 56,
          alignItems: 'start',
        }}
      >
        {workflows}
        {feed}
      </div>
    </div>
  );
}
