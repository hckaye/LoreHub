"use client";

import { ChevronDown, Plus } from "lucide-react";
import Link from "next/link";

import { PopupMenu } from "@/components/ui/popup-menu";
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
    <PopupMenu
      className={styles.menu}
      panelClassName={styles.dropdown}
      trigger={
        <>
          <Plus aria-hidden="true" size={17} />
          {!compact && <span>{dictionary.common.create}</span>}
          {compact && <ChevronDown aria-hidden="true" size={12} />}
        </>
      }
      triggerClassName={styles.trigger}
      triggerProps={{ "aria-label": dictionary.common.create }}
    >
      {(close) => (
        <>
          <Link href={`/${locale}/issues/new`} onClick={close} role="menuitem">
            {dictionary.common.newIssue}
          </Link>
          <Link href={`/${locale}/pulls/new`} onClick={close} role="menuitem">
            {dictionary.common.newPullRequest}
          </Link>
          <Link href={`/${locale}/organizations/new`} onClick={close} role="menuitem">
            {dictionary.common.newOrganization}
          </Link>
          <Link href={`/${locale}/repositories/new`} onClick={close} role="menuitem">
            {dictionary.common.newRepository}
          </Link>
        </>
      )}
    </PopupMenu>
  );
}
