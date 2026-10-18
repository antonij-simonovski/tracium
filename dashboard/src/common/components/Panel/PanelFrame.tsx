import type { ReactNode } from 'react';
import { useMaxWidth, BREAKPOINTS } from '../../hooks/useMediaQuery';
import styles from './PanelFrame.module.css';

interface PanelFrameProps {
  left: ReactNode;
  right?: ReactNode;
  columns?: string;
}

export function PanelFrame({ left, right, columns = '1.4fr 1fr' }: PanelFrameProps) {
  const stacked = useMaxWidth(BREAKPOINTS.tablet);
  const single = stacked || !right;
  return (
    <div className={styles.frame} style={{ gridTemplateColumns: single ? '1fr' : columns }}>
      <div className={`${styles.cell} ${right ? (stacked ? styles.stackDivider : styles.divider) : ''}`}>{left}</div>
      {right && <div className={styles.cell}>{right}</div>}
    </div>
  );
}
