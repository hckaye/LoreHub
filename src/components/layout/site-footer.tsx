import type { Dictionary } from "@/i18n";

import styles from "./site-footer.module.css";

type SiteFooterProps = {
  dictionary: Dictionary;
};

export function SiteFooter({ dictionary }: SiteFooterProps) {
  return (
    <footer className={styles.footer}>
      <div className={styles.inner}>
        <span className={styles.copyright}>© {new Date().getFullYear()}</span>
        <span className={styles.product}>{dictionary.common.productName}</span>
        <div className={styles.links}>
          <a href="https://github.com/EpicGames/lore" rel="noreferrer" target="_blank">
            {dictionary.common.documentation}
          </a>
          <span>{dictionary.common.license}</span>
        </div>
      </div>
    </footer>
  );
}
