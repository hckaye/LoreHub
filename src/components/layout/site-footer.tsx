import type { Dictionary } from "@/i18n";

import styles from "./site-footer.module.css";

type SiteFooterProps = {
  dictionary: Dictionary;
};

export function SiteFooter({ dictionary }: SiteFooterProps) {
  return (
    <footer className={styles.footer}>
      <div className={styles.inner}>
        <span>
          © {new Date().getFullYear()} {dictionary.common.productName}
        </span>
        <p>MIT License</p>
      </div>
    </footer>
  );
}
