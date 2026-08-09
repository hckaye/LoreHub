import type { ReactNode } from "react";

import styles from "./section-heading.module.css";

type SectionHeadingProps = {
  title: string;
  description?: string;
  actions?: ReactNode;
};

export function SectionHeading({ title, description, actions }: SectionHeadingProps) {
  return (
    <div className={styles.heading}>
      <div>
        <h1>{title}</h1>
        {description && <p>{description}</p>}
      </div>
      {actions && <div className={styles.actions}>{actions}</div>}
    </div>
  );
}
