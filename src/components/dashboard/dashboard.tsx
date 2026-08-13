import { Activity, Bell, BookOpenText, Package, Plus } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { DashboardData } from "@/lib/api-types";
import { repositoryPath } from "@/lib/routes";

import { RepositoryCard } from "../repositories/repository-card";
import { EmptyState } from "../ui/empty-state";
import styles from "./dashboard.module.css";

type DashboardProps = {
  locale: Locale;
  dictionary: Dictionary;
  dashboard: DashboardData | null;
  unavailable: boolean;
  userName: string;
};

export function Dashboard({ dashboard, locale, dictionary, unavailable, userName }: DashboardProps) {
  return (
    <div className={styles.page}>
      <div className={styles.layout}>
        <DashboardSidebar
          dashboard={dashboard}
          dictionary={dictionary}
          locale={locale}
          unavailable={unavailable}
          userName={userName}
        />
        <div className={styles.main}>
          <h1>{dictionary.home.homeTitle}</h1>
          <DashboardActivity dashboard={dashboard} dictionary={dictionary} locale={locale} unavailable={unavailable} />
        </div>
        <DashboardExplore dashboard={dashboard} dictionary={dictionary} locale={locale} unavailable={unavailable} />
      </div>
    </div>
  );
}

type DashboardSidebarProps = {
  dashboard: DashboardData | null;
  dictionary: Dictionary;
  locale: Locale;
  unavailable: boolean;
  userName: string;
};

function DashboardSidebar(props: DashboardSidebarProps) {
  const repositories = props.dashboard?.repositories ?? [];
  const organizations = props.dashboard?.organizations ?? [];
  const visibleRepositories = repositories.slice(0, 12);
  return (
    <aside className={styles.sidebar}>
      <p className={styles.identity}>{props.userName}</p>
      <div className={styles.sidebarHeading}>
        <h2>{props.dictionary.common.repositories}</h2>
        <Link href={`/${props.locale}/repositories/new`}>
          <Plus aria-hidden="true" size={15} />
          {props.dictionary.common.newRepository}
        </Link>
      </div>
      {visibleRepositories.length > 0 ? (
        <>
          <ul className={styles.repositoryList}>
            {visibleRepositories.map((repository) => (
              <li key={repository.id}>
                <Link href={repositoryPath(props.locale, repository.owner, repository.slug)}>
                  <Package aria-hidden="true" size={16} />
                  <span>{repository.owner}</span>/<strong>{repository.slug}</strong>
                </Link>
              </li>
            ))}
          </ul>
          {repositories.length > visibleRepositories.length && (
            <Link className={styles.showMore} href={`/${props.locale}/profile`}>
              {props.dictionary.home.showMore}
            </Link>
          )}
        </>
      ) : (
        <p className={styles.sidebarEmpty}>
          {props.unavailable
            ? props.dictionary.home.apiUnavailableBody
            : props.dictionary.home.availablePublicRepositoriesEmptyBody}
        </p>
      )}
      <h2 className={styles.organizationsTitle}>{props.dictionary.common.organizations}</h2>
      {organizations.length > 0 ? (
        <ul className={styles.organizationList}>
          {organizations.map((organization) => (
            <li key={organization.id}>
              <Link href={`/${props.locale}/organizations/${encodeURIComponent(organization.slug)}`}>
                <span aria-hidden="true">{organization.displayName.slice(0, 1).toLocaleUpperCase()}</span>
                {organization.displayName}
              </Link>
            </li>
          ))}
        </ul>
      ) : (
        <p className={styles.sidebarEmpty}>
          {props.unavailable
            ? props.dictionary.home.apiUnavailableBody
            : props.dictionary.home.organizationsEmptyBody}
        </p>
      )}
    </aside>
  );
}

type DashboardActivityProps = {
  dashboard: DashboardData | null;
  dictionary: Dictionary;
  locale: Locale;
  unavailable: boolean;
};

function DashboardActivity({ dashboard, dictionary, locale, unavailable }: DashboardActivityProps) {
  const notifications = dashboard?.notifications ?? [];
  return (
    <section className={styles.activity}>
      <div className={styles.sectionTitle}>
        <Bell aria-hidden="true" size={18} />
        <h2>{dictionary.home.activityTitle}</h2>
      </div>
      {notifications.length > 0 ? (
        <ul className={styles.notificationList}>
          {notifications.map((notification) => (
            <li data-unread={!notification.readAt} key={notification.id}>
              <Link href={localizeHref(notification.href, locale)}>
                <strong>{notification.title}</strong>
                <span>{notification.body || notification.topic}</span>
              </Link>
            </li>
          ))}
        </ul>
      ) : (
        <EmptyState
          body={unavailable ? dictionary.home.apiUnavailableBody : dictionary.home.activityEmptyBody}
          icon={<Activity aria-hidden="true" />}
          title={unavailable ? dictionary.home.apiUnavailableTitle : dictionary.home.activityEmptyTitle}
          tone={unavailable ? "warning" : "neutral"}
        />
      )}
    </section>
  );
}

type DashboardExploreProps = {
  dashboard: DashboardData | null;
  dictionary: Dictionary;
  locale: Locale;
  unavailable: boolean;
};

function DashboardExplore({ dashboard, dictionary, locale, unavailable }: DashboardExploreProps) {
  const repositories = dashboard?.repositories.filter((repository) => repository.visibility === "public") ?? [];
  return (
    <aside className={styles.explore}>
      <div className={styles.sectionTitle}>
        <h2>{dictionary.home.discoverTitle}</h2>
      </div>
      {unavailable ? (
        <p className={styles.sidebarEmpty}>{dictionary.home.apiUnavailableBody}</p>
      ) : repositories.length > 0 ? (
        <div className={styles.cards}>
          {repositories.slice(0, 4).map((repository) => (
            <RepositoryCard dictionary={dictionary} key={repository.id} locale={locale} repository={repository} />
          ))}
        </div>
      ) : (
        <p className={styles.sidebarEmpty}>{dictionary.home.discoverEmptyBody}</p>
      )}
      <a className={styles.docsLink} href="https://github.com/EpicGames/lore" rel="noreferrer" target="_blank">
        <BookOpenText aria-hidden="true" size={16} />
        {dictionary.home.architecture}
      </a>
    </aside>
  );
}

function localizeHref(href: string, locale: Locale): string {
  if (href === "/" || href.startsWith(`/${locale}/`)) {
    return href === "/" ? `/${locale}` : href;
  }
  return `/${locale}${href.startsWith("/") ? href : `/${href}`}`;
}
