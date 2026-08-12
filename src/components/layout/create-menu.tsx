import { ChevronDown, Plus } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";

import styles from "./create-menu.module.css";

type CreateMenuProps = {
  locale: Locale;
  dictionary: Dictionary;
  compact?: boolean;
};

export function CreateMenu({ locale, dictionary, compact = false }: CreateMenuProps) {
  return (
    <details className={styles.menu}>
      <summary aria-label={dictionary.common.create} className={styles.summary}>
        <Plus aria-hidden="true" size={17} />
        {!compact && <span>{dictionary.common.create}</span>}
        {compact && <ChevronDown aria-hidden="true" size={12} />}
      </summary>
      <div className={styles.dropdown}>
        <Link href={`/${locale}/issues/new`}>{dictionary.common.newIssue}</Link>
        <Link href={`/${locale}/pulls/new`}>{dictionary.common.newPullRequest}</Link>
        <Link href={`/${locale}/organizations/new`}>{dictionary.common.newOrganization}</Link>
        <Link href={`/${locale}/repositories/new`}>{dictionary.common.newRepository}</Link>
      </div>
    </details>
  );
}
