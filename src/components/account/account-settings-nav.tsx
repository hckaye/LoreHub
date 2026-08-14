"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import type { ChangeEvent } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import { accountSettingsPath, accountSettingsSection } from "@/lib/routes";

import styles from "./account-settings.module.css";

type AccountSettingsNavProps = {
  dictionary: Dictionary;
  locale: Locale;
  showEntitlements: boolean;
  showInstanceSettings: boolean;
};

type NavLink = {
  href: string;
  label: string;
  section: string;
};

export function AccountSettingsNav({
  dictionary,
  locale,
  showEntitlements,
  showInstanceSettings,
}: AccountSettingsNavProps) {
  const pathname = usePathname() ?? "";
  const router = useRouter();
  const section = accountSettingsSection(pathname);
  const copy = dictionary.settingsNav;
  const developer = isDeveloperSection(section);

  const profile = item(locale, "profile", copy.publicProfile);
  const notifications = item(locale, "notifications", copy.notifications);
  const invitations = item(locale, "repositories", copy.repositoryInvitations);
  const developerSettings = item(locale, "tokens", copy.developerSettings);
  const tokensClassic = item(locale, "tokens", copy.tokensClassic);
  const entitlements = item(locale, "entitlements", copy.entitlements);
  const instance = item(locale, "instance", copy.instanceSettings);

  const mainOptions = [profile, notifications, invitations, developerSettings];
  if (showEntitlements) mainOptions.push(entitlements);
  if (showInstanceSettings) mainOptions.push(instance);
  const settingsHome = item(locale, "profile", copy.backToSettings);
  const options = developer ? [settingsHome, tokensClassic] : mainOptions;

  function changePage(event: ChangeEvent<HTMLSelectElement>) {
    router.push(event.target.value);
  }

  return (
    <aside className={styles.sidebar}>
      <label className="visually-hidden" htmlFor="account-settings-section">
        {developer ? copy.developerSettings : copy.userSettings}
      </label>
      <select
        className={styles.mobileSelect}
        id="account-settings-section"
        onChange={changePage}
        value={options.find((option) => isActive(section, option.section))?.href ?? options[0]?.href}
      >
        {options.map((option) => (
          <option key={option.href} value={option.href}>
            {option.label}
          </option>
        ))}
      </select>
      {developer ? (
        <nav aria-label={copy.developerSettings} className={styles.nav}>
          <NavItem active={false} href={settingsHome.href} label={settingsHome.label} />
          <div className={styles.divider} />
          <h2 className={styles.group}>{copy.personalAccessTokens}</h2>
          <NavItem
            active={section === "tokens" || section.startsWith("tokens/")}
            href={tokensClassic.href}
            label={tokensClassic.label}
            nested
          />
        </nav>
      ) : (
        <nav aria-label={copy.userSettings} className={styles.nav}>
          <NavItem active={section === "profile"} href={profile.href} label={profile.label} />
          <NavItem active={section === "notifications"} href={notifications.href} label={notifications.label} />
          <h2 className={styles.group}>{copy.access}</h2>
          <NavItem active={section === "repositories"} href={invitations.href} label={invitations.label} />
          <div className={styles.divider} />
          <NavItem active={false} href={developerSettings.href} label={developerSettings.label} />
          {showEntitlements || showInstanceSettings ? <div className={styles.divider} /> : null}
          {showEntitlements ? (
            <NavItem active={section === "entitlements"} href={entitlements.href} label={entitlements.label} />
          ) : null}
          {showInstanceSettings ? (
            <NavItem active={section === "instance"} href={instance.href} label={instance.label} />
          ) : null}
        </nav>
      )}
    </aside>
  );
}

function NavItem({ active, href, label, nested }: { active: boolean; href: string; label: string; nested?: boolean }) {
  return (
    <Link
      aria-current={active ? "page" : undefined}
      className={nested ? `${styles.item} ${styles.nested}` : styles.item}
      href={href}
    >
      {label}
    </Link>
  );
}

function item(
  locale: Locale,
  section: "profile" | "notifications" | "repositories" | "tokens" | "entitlements" | "instance",
  label: string,
): NavLink {
  return { href: accountSettingsPath(locale, section), label, section };
}

function isDeveloperSection(section: string): boolean {
  return section === "tokens" || section.startsWith("tokens/");
}

function isActive(current: string, section: string): boolean {
  return current === section || current.startsWith(`${section}/`);
}
