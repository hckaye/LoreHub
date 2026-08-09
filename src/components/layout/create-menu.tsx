import { Plus } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";

import styles from "./create-menu.module.css";

type CreateMenuProps = {
  locale: Locale;
  dictionary: Dictionary;
};

export function CreateMenu({ locale, dictionary }: CreateMenuProps) {
  return (
    <details className={styles.menu}>
      <summary aria-label={dictionary.common.create} className={styles.summary}>
        <Plus aria-hidden="true" size={17} />
        <span>{dictionary.common.create}</span>
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
