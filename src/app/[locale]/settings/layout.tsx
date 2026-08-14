import type { ReactNode } from "react";

import { AccountSettingsShell } from "@/components/account/account-settings-shell";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getEntitlements } from "@/lib/lorehub-api";

import styles from "@/components/account/account-settings.module.css";

type SettingsLayoutProps = {
  children: ReactNode;
  params: Promise<{ locale: string }>;
};

export default async function SettingsLayout({ children, params }: SettingsLayoutProps) {
  const { locale: value } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  if (session.status !== "authenticated") {
    return <div className={styles.page}>{children}</div>;
  }
  const entitlements = await getEntitlements();
  return (
    <AccountSettingsShell dictionary={dictionary} locale={locale} showEntitlements={entitlements.ok}>
      {children}
    </AccountSettingsShell>
  );
}
