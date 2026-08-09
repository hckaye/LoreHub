import type { ReactNode } from "react";

import styles from "./empty-state.module.css";

type EmptyStateProps = {
  icon: ReactNode;
  title: string;
  body: string;
  tone?: "neutral" | "warning";
};

export function EmptyState({ icon, title, body, tone = "neutral" }: EmptyStateProps) {
  return (
    <div className={styles.empty} data-tone={tone}>
      <div className={styles.icon}>{icon}</div>
      <div>
        <h3>{title}</h3>
        <p>{body}</p>
      </div>
    </div>
  );
}
