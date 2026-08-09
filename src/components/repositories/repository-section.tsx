import type { ReactNode } from "react";

import { SectionHeading } from "../ui/section-heading";
import styles from "./repository-section.module.css";

type RepositorySectionProps = {
  title: string;
  description: string;
  actions?: ReactNode;
  children: ReactNode;
};

export function RepositorySection({ title, description, actions, children }: RepositorySectionProps) {
  return (
    <div className={styles.page}>
      <SectionHeading actions={actions} description={description} title={title} />
      {children}
    </div>
  );
}

type RepositoryPanelProps = {
  title: string;
  description?: string;
  actions?: ReactNode;
  children: ReactNode;
};

export function RepositoryPanel({ title, description, actions, children }: RepositoryPanelProps) {
  return (
    <section className={styles.panel}>
      <div className={styles.panelHeading}>
        <div>
          <h2>{title}</h2>
          {description && <p>{description}</p>}
        </div>
        {actions && <div className={styles.panelActions}>{actions}</div>}
      </div>
      {children}
    </section>
  );
}
