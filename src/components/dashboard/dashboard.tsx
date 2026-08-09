import { Activity, Bell, BookOpenText, ServerOff } from "lucide-react";
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
  const greeting = dictionary.home.dashboardGreeting.replace("{name}", userName);
  return (
    <div className={styles.page}>
      <div className={styles.heading}>
        <div>
          <p className={styles.eyebrow}>{dictionary.home.dashboardTitle}</p>
          <h1>{greeting}</h1>
          <p>{dictionary.home.dashboardDescription}</p>
        </div>
        <Link className={styles.docsLink} href="https://github.com/EpicGames/lore" rel="noreferrer" target="_blank">
          <BookOpenText aria-hidden="true" size={17} />
          {dictionary.home.architecture}
        </Link>
      </div>
      <div className={styles.layout}>
        <DashboardSidebar dashboard={dashboard} dictionary={dictionary} locale={locale} unavailable={unavailable} />
        <div className={styles.main}>
          <DashboardActivity dashboard={dashboard} dictionary={dictionary} unavailable={unavailable} />
          <section>
            <div className={styles.sectionTitle}>
              <h2>{dictionary.home.discoverTitle}</h2>
              <span>{dictionary.home.publicDashboardNote}</span>
            </div>
            {unavailable ? (
              <EmptyState
                body={dictionary.home.apiUnavailableBody}
                icon={<ServerOff aria-hidden="true" />}
                title={dictionary.home.apiUnavailableTitle}
                tone="warning"
              />
            ) : (
              <div className={styles.cards}>
                {(dashboard?.repositories ?? []).map((repository) => (
                  <RepositoryCard dictionary={dictionary} key={repository.id} locale={locale} repository={repository} />
                ))}
              </div>
            )}
          </section>
        </div>
      </div>
    </div>
  );
}

type DashboardSidebarProps = {
  dashboard: DashboardData | null;
  dictionary: Dictionary;
  locale: Locale;
  unavailable: boolean;
};

function DashboardSidebar({ dashboard, dictionary, locale, unavailable }: DashboardSidebarProps) {
  const organizations = dashboard?.organizations ?? [];
  const repositories = dashboard?.repositories ?? [];
  let content = null;
  if (organizations.length > 0) {
    content = (
      <ul className={styles.repositoryList}>
        {organizations.map((organization) => (
          <li key={organization.id}>
            <Link href={`/${locale}/organizations/${encodeURIComponent(organization.slug)}`}>
              <strong>{organization.displayName}</strong>
              <span>{organization.slug}</span>
            </Link>
          </li>
        ))}
      </ul>
    );
  } else if (repositories.length > 0) {
    content = (
      <ul className={styles.repositoryList}>
        {repositories.map((repository) => (
          <li key={repository.id}>
            <Link href={repositoryPath(locale, repository.owner, repository.slug)}>
              <strong>{repository.displayName}</strong>
              <span>{repository.owner}</span>
            </Link>
          </li>
        ))}
      </ul>
    );
  } else {
    content = (
      <EmptyState
        body={unavailable ? dictionary.home.apiUnavailableBody : dictionary.home.availablePublicRepositoriesEmptyBody}
        icon={<ServerOff aria-hidden="true" />}
        title={
          unavailable ? dictionary.home.apiUnavailableTitle : dictionary.home.availablePublicRepositoriesEmptyTitle
        }
        tone={unavailable ? "warning" : "neutral"}
      />
    );
  }
  return (
    <aside className={styles.sidebar}>
      <div className={styles.sidebarHeading}>
        <h2>{dictionary.common.organizations}</h2>
        <p>{dictionary.home.dashboardDescription}</p>
      </div>
      {content}
    </aside>
  );
}

type DashboardActivityProps = {
  dashboard: DashboardData | null;
  dictionary: Dictionary;
  unavailable: boolean;
};

function DashboardActivity({ dashboard, dictionary, unavailable }: DashboardActivityProps) {
  const notifications = dashboard?.notifications ?? [];
  let content = null;
  if (notifications.length > 0) {
    content = (
      <ul className={styles.notificationList}>
        {notifications.map((notification) => (
          <li data-unread={!notification.readAt} key={notification.id}>
            <Link href={notification.href}>
              <strong>{notification.title}</strong>
              <span>{notification.body || notification.topic}</span>
            </Link>
          </li>
        ))}
      </ul>
    );
  } else {
    content = (
      <EmptyState
        body={unavailable ? dictionary.home.apiUnavailableBody : dictionary.home.activityEmptyBody}
        icon={<Activity aria-hidden="true" />}
        title={unavailable ? dictionary.home.apiUnavailableTitle : dictionary.home.activityEmptyTitle}
        tone={unavailable ? "warning" : "neutral"}
      />
    );
  }
  return (
    <section className={styles.activity}>
      <div className={styles.sectionTitle}>
        <Bell aria-hidden="true" size={18} />
        <h2>{dictionary.home.activityTitle}</h2>
      </div>
      {content}
    </section>
  );
}
