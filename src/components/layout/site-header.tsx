"use client";

import { Bell, BookOpen, CircleDot, GitPullRequest, Home, Menu, Search, X } from "lucide-react";
import Link from "next/link";
import { usePathname, useSearchParams } from "next/navigation";
import { useCallback, useRef, useState } from "react";

import { useDismissOnOutsideInteraction } from "@/components/ui/popup-menu";
import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession } from "@/lib/api-types";
import { abbreviateCount } from "@/lib/format";
import { brandedAuthUrl } from "@/lib/routes";

import { AccountMenu } from "./account-menu";
import { CreateMenu } from "./create-menu";
import { LocaleSwitcher } from "./locale-switcher";
import styles from "./site-header.module.css";

type SiteHeaderProps = {
  locale: Locale;
  dictionary: Dictionary;
  session: AuthSession;
  unreadNotifications: number;
};

export function SiteHeader({ locale, dictionary, session, unreadNotifications }: SiteHeaderProps) {
  const [navigationOpen, setNavigationOpen] = useState(false);
  const navigationRef = useRef<HTMLElement>(null);
  const navigationToggleRef = useRef<HTMLButtonElement>(null);
  const closeNavigation = useCallback(() => setNavigationOpen(false), []);
  useDismissOnOutsideInteraction({
    close: closeNavigation,
    containers: [navigationRef, navigationToggleRef],
    focusOnDismiss: navigationToggleRef,
    open: navigationOpen,
  });
  const pathname = usePathname() ?? `/${locale}`;
  const searchParams = useSearchParams();
  const searchQuery = searchParams.get("q") ?? "";
  const returnTo = `${pathname}${searchParams.toString() ? `?${searchParams.toString()}` : ""}`;
  const isAuthenticated = session.status === "authenticated";
  const notificationCount = unreadNotifications > 99 ? "99+" : abbreviateCount(unreadNotifications, locale);
  const notificationLabel =
    unreadNotifications > 0
      ? dictionary.common.unreadNotifications.replace("{count}", unreadNotifications.toLocaleString(locale))
      : dictionary.common.notifications;

  return (
    <header className={styles.header} data-authenticated={isAuthenticated}>
      <div className={styles.inner}>
        <button
          aria-controls="primary-navigation"
          aria-expanded={navigationOpen}
          aria-label={navigationOpen ? dictionary.common.closeMenu : dictionary.common.openMenu}
          className={styles.menuButton}
          onClick={() => setNavigationOpen((open) => !open)}
          ref={navigationToggleRef}
          type="button"
        >
          {navigationOpen ? <X aria-hidden="true" size={18} /> : <Menu aria-hidden="true" size={18} />}
        </button>
        <Link
          aria-label={dictionary.common.productName}
          className={styles.brand}
          href={`/${locale}`}
          onClick={() => setNavigationOpen(false)}
        >
          <span aria-hidden="true" className={styles.mark}>
            L
          </span>
          <span className={styles.brandName}>{dictionary.common.productName}</span>
        </Link>
        <nav
          aria-label={dictionary.common.primaryNavigation}
          className={`${styles.navigation} ${navigationOpen ? styles.navigationOpen : ""}`}
          id="primary-navigation"
          ref={navigationRef}
        >
          <Link href={`/${locale}`} onClick={() => setNavigationOpen(false)}>
            <Home aria-hidden="true" size={16} />
            {isAuthenticated ? dictionary.common.dashboard : dictionary.common.explore}
          </Link>
          {isAuthenticated && (
            <>
              <Link className={styles.mobileOnly} href={`/${locale}/issues`} onClick={() => setNavigationOpen(false)}>
                <CircleDot aria-hidden="true" size={16} />
                {dictionary.common.issues}
              </Link>
              <Link className={styles.mobileOnly} href={`/${locale}/pulls`} onClick={() => setNavigationOpen(false)}>
                <GitPullRequest aria-hidden="true" size={16} />
                {dictionary.common.pullRequests}
              </Link>
              <Link
                aria-label={notificationLabel}
                className={styles.mobileOnly}
                href={`/${locale}/notifications`}
                onClick={() => setNavigationOpen(false)}
              >
                <Bell aria-hidden="true" size={16} />
                {dictionary.common.notifications}
                {unreadNotifications > 0 && (
                  <span aria-hidden="true" className={styles.notificationCount}>
                    {notificationCount}
                  </span>
                )}
              </Link>
            </>
          )}
          <a href="https://github.com/EpicGames/lore" rel="noreferrer" target="_blank">
            <BookOpen aria-hidden="true" size={16} />
            {dictionary.common.documentation}
          </a>
          <span className={styles.mobileLocale}>
            <LocaleSwitcher dictionary={dictionary} locale={locale} onClick={() => setNavigationOpen(false)} />
          </span>
        </nav>
        <form action={`/${locale}/search`} className={styles.search} role="search">
          <label className="visually-hidden" htmlFor="global-repository-search">
            {dictionary.common.searchRepositories}
          </label>
          <Search aria-hidden="true" size={16} />
          <input
            defaultValue={searchQuery}
            id="global-repository-search"
            key={searchQuery}
            name="q"
            placeholder={dictionary.common.searchPlaceholder}
            type="search"
          />
        </form>
        <div className={styles.actions}>
          {isAuthenticated ? (
            <>
              <Link
                aria-label={dictionary.common.issues}
                className={styles.iconAction}
                href={`/${locale}/issues`}
                title={dictionary.common.issues}
              >
                <CircleDot aria-hidden="true" size={16} />
              </Link>
              <Link
                aria-label={dictionary.common.pullRequests}
                className={styles.iconAction}
                href={`/${locale}/pulls`}
                title={dictionary.common.pullRequests}
              >
                <GitPullRequest aria-hidden="true" size={16} />
              </Link>
              <Link
                aria-label={notificationLabel}
                className={styles.iconAction}
                href={`/${locale}/notifications`}
                title={notificationLabel}
              >
                <Bell aria-hidden="true" size={16} />
                {unreadNotifications > 0 && (
                  <span aria-hidden="true" className={styles.notificationDot}>
                    {notificationCount}
                  </span>
                )}
              </Link>
              <CreateMenu compact dictionary={dictionary} locale={locale} />
              <AccountMenu dictionary={dictionary} locale={locale} session={session} />
            </>
          ) : (
            <div className={styles.authLinks}>
              <Link href={brandedAuthUrl(locale, returnTo)}>{dictionary.common.signIn}</Link>
              <Link className={styles.signUp} href={brandedAuthUrl(locale, returnTo, true)}>
                {dictionary.common.signUp}
              </Link>
            </div>
          )}
          <span className={styles.localeAction}>
            <LocaleSwitcher dictionary={dictionary} locale={locale} />
          </span>
        </div>
      </div>
    </header>
  );
}
