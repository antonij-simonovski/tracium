import type { CSSProperties, ReactNode, Ref } from 'react';
import styles from './SectionHead.module.css';

export interface SectionHeadProps {
  title: string;
  hint?: string;
  right?: ReactNode;
  first?: boolean;
  headingRef?: Ref<HTMLHeadingElement>;
  style?: CSSProperties;
}

export function SectionHead({ title, hint, right, first = false, headingRef, style }: SectionHeadProps) {
  return (
    <header className={`${styles.head} ${first ? styles.first : ''}`} style={style}>
      <div className={styles.text}>
        <h2 ref={headingRef} tabIndex={headingRef ? -1 : undefined} className={styles.title}>
          {title}
        </h2>
        {hint && <p className={styles.hint}>{hint}</p>}
      </div>
      {right && <div className={styles.right}>{right}</div>}
    </header>
  );
}
