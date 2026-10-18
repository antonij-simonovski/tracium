import type { ReactNode } from 'react';
import { Centered } from './Centered';
import { Spinner } from './Spinner';

interface SectionProps {
  isLoading: boolean;
  isError: boolean;
  minHeight?: number;
  children: ReactNode;
}

export function Section({ isLoading, isError, minHeight = 120, children }: SectionProps) {
  if (isLoading) {
    return (
      <Centered minHeight={minHeight}>
        <Spinner />
      </Centered>
    );
  }
  if (isError) {
    return <Centered minHeight={minHeight}>Failed to load</Centered>;
  }
  return <>{children}</>;
}
