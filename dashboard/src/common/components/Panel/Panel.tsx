import type { ReactNode } from 'react';
import styles from './Panel.module.css';

export interface PanelProps {
  eyebrow: string;
  title: string;
  right?: ReactNode;
  // Cap the content to the height the frame gives the panel and scroll inside
  // it, so a long list matches the chart beside it. Only useful side by side.
  scroll?: boolean;
  children: ReactNode;
}

export function Panel({ eyebrow, title, right, scroll = false, children }: PanelProps) {
  return (
    <div className={styles.panel}>
      <div className={styles.header}>
        <div className={styles.heading}>
          <span className={styles.eyebrow}>{eyebrow}</span>
          <span className={styles.title}>{title}</span>
        </div>
        {right && <div className={styles.right}>{right}</div>}
      </div>
      <div className={`${styles.content} ${scroll ? styles.scrollHost : ''}`}>
        {scroll ? <div className={styles.scroller}>{children}</div> : children}
      </div>
    </div>
  );
}
