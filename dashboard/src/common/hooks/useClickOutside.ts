import { useEffect } from 'react';

export function useClickOutside(ref: React.RefObject<HTMLElement | null>, open: boolean, onOutside: () => void): void {
  useEffect(() => {
    if (!open) return;
    const h = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onOutside();
    };
    document.addEventListener('mousedown', h);
    return () => document.removeEventListener('mousedown', h);
  }, [ref, open, onOutside]);
}
