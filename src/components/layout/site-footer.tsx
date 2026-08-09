import type { Dictionary } from "@/i18n";

import styles from "./site-footer.module.css";

type SiteFooterProps = {
  dictionary: Dictionary;
};

export function SiteFooter({ dictionary }: SiteFooterProps) {
  return (
    <footer className={styles.footer}>
      <div className={styles.inner}>
        <span>{dictionary.common.productName}</span>
        <p>{dictionary.footer.sourceOfTruth}</p>
      </div>
    </footer>
  );
}
