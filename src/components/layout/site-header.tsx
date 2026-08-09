import { BookOpen, Search } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";

import { LocaleSwitcher } from "./locale-switcher";
import styles from "./site-header.module.css";

type SiteHeaderProps = {
  locale: Locale;
  dictionary: Dictionary;
};

export function SiteHeader({ locale, dictionary }: SiteHeaderProps) {
  return (
    <header className={styles.header}>
      <div className={styles.inner}>
        <Link className={styles.brand} href={`/${locale}`}>
          <span aria-hidden="true" className={styles.mark}>
            L
          </span>
          <span>{dictionary.common.productName}</span>
        </Link>
        <nav aria-label={dictionary.common.primaryNavigation} className={styles.navigation}>
          <Link href={`/${locale}`}>
            <Search aria-hidden="true" size={16} />
            {dictionary.common.explore}
          </Link>
          <a href="https://epicgames.github.io/lore/" rel="noreferrer" target="_blank">
            <BookOpen aria-hidden="true" size={16} />
            {dictionary.common.documentation}
          </a>
        </nav>
        <div className={styles.actions}>
          <LocaleSwitcher locale={locale} />
        </div>
      </div>
    </header>
  );
}
