import type { ReactNode } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";

import { AccountSettingsNav } from "./account-settings-nav";
import styles from "./account-settings.module.css";

type AccountSettingsShellProps = {
  children: ReactNode;
  dictionary: Dictionary;
  locale: Locale;
  showEntitlements: boolean;
  showInstanceSettings: boolean;
};

export function AccountSettingsShell({
  children,
  dictionary,
  locale,
  showEntitlements,
  showInstanceSettings,
}: AccountSettingsShellProps) {
  return (
    <div className={styles.page}>
      <div className={styles.layout}>
        <AccountSettingsNav
          dictionary={dictionary}
          locale={locale}
          showEntitlements={showEntitlements}
          showInstanceSettings={showInstanceSettings}
        />
        <div className={styles.main}>{children}</div>
      </div>
    </div>
  );
}

type AccountSettingsHeaderProps = {
  title: string;
  description?: string;
  actions?: ReactNode;
};

export function AccountSettingsHeader({ actions, description, title }: AccountSettingsHeaderProps) {
  return (
    <div className={styles.header}>
      <div className={styles.headerRow}>
        <h1 className={styles.title}>{title}</h1>
        {actions ? <div className={styles.headerActions}>{actions}</div> : null}
      </div>
      {description ? <p className={styles.lede}>{description}</p> : null}
    </div>
  );
}
