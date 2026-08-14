import type { ReactNode } from "react";

import styles from "./blankslate.module.css";

type BlankslateProps = {
  icon: ReactNode;
  title: string;
  body?: string;
  action?: ReactNode;
};

export function Blankslate({ icon, title, body, action }: BlankslateProps) {
  return (
    <div className={styles.blankslate}>
      <div className={styles.icon}>{icon}</div>
      <h3 className={styles.title}>{title}</h3>
      {body && <p className={styles.body}>{body}</p>}
      {action && <div className={styles.action}>{action}</div>}
    </div>
  );
}
