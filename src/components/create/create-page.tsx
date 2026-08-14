import type { ReactNode } from "react";

import styles from "./create-page.module.css";

type CreatePageProps = {
  title?: string;
  description?: string;
  children: ReactNode;
  wide?: boolean;
};

export function CreatePage({ children, description, title, wide = false }: CreatePageProps) {
  return (
    <div className={wide ? `${styles.page} ${styles.wide}` : styles.page}>
      {title ? <h1>{title}</h1> : null}
      {description ? <p className={styles.lede}>{description}</p> : null}
      {children}
    </div>
  );
}
