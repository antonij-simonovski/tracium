import { useState, useEffect } from 'react';

export const BREAKPOINTS = {
  mobile: 900,
  tablet: 1100,
} as const;

function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() =>
    typeof window !== 'undefined' ? window.matchMedia(query).matches : false,
  );

  useEffect(() => {
    const mql = window.matchMedia(query);
    const onChange = () => setMatches(mql.matches);
    onChange();
    mql.addEventListener('change', onChange);
    return () => mql.removeEventListener('change', onChange);
  }, [query]);

  return matches;
}

export function useMaxWidth(maxWidth: number): boolean {
  return useMediaQuery(`(max-width: ${maxWidth}px)`);
}
