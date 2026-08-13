import type { ReactNode } from "react";

import styles from "./flash-notice.module.css";

type FlashNoticeProps = {
  title: string;
  body?: string;
  tone?: "info" | "warning" | "success" | "error";
  icon?: ReactNode;
};

export function FlashNotice({ title, body, tone = "info", icon }: FlashNoticeProps) {
  return (
    <aside aria-live="polite" className={styles.notice} data-tone={tone}>
      {icon && <span className={styles.icon}>{icon}</span>}
      <div>
        <strong>{title}</strong>
        {body && <p>{body}</p>}
      </div>
    </aside>
  );
}
