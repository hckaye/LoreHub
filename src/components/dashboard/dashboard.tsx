import { Activity, BookOpenText, ServerOff } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { Repository } from "@/lib/api-types";
import { repositoryPath } from "@/lib/routes";

import { RepositoryCard } from "../repositories/repository-card";
import { EmptyState } from "../ui/empty-state";
import styles from "./dashboard.module.css";

type DashboardProps = {
  locale: Locale;
  dictionary: Dictionary;
  repositories: Repository[] | null;
  repositoriesUnavailable: boolean;
  userName: string;
};

export function Dashboard({ locale, dictionary, repositories, repositoriesUnavailable, userName }: DashboardProps) {
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
        <aside className={styles.sidebar}>
          <div className={styles.sidebarHeading}>
            <div>
              <h2>{dictionary.home.yourRepositories}</h2>
              <p>{dictionary.home.yourRepositoriesDescription}</p>
            </div>
          </div>
          {!repositoriesUnavailable && repositories && repositories.length > 0 ? (
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
          ) : (
            <EmptyState
              body={
                repositoriesUnavailable ? dictionary.home.apiUnavailableBody : dictionary.home.yourRepositoriesEmptyBody
              }
              icon={<ServerOff aria-hidden="true" />}
              title={
                repositoriesUnavailable
                  ? dictionary.home.apiUnavailableTitle
                  : dictionary.home.yourRepositoriesEmptyTitle
              }
              tone={repositoriesUnavailable ? "warning" : "neutral"}
            />
          )}
        </aside>
        <main className={styles.main}>
          <section className={styles.activity}>
            <div className={styles.sectionTitle}>
              <Activity aria-hidden="true" size={18} />
              <h2>{dictionary.home.activityTitle}</h2>
            </div>
            <EmptyState
              body={dictionary.home.activityEmptyBody}
              icon={<Activity aria-hidden="true" />}
              title={dictionary.home.activityEmptyTitle}
            />
          </section>
          <section>
            <div className={styles.sectionTitle}>
              <h2>{dictionary.home.discoverTitle}</h2>
              <span>{dictionary.home.publicDashboardNote}</span>
            </div>
            {repositoriesUnavailable ? (
              <EmptyState
                body={dictionary.home.apiUnavailableBody}
                icon={<ServerOff aria-hidden="true" />}
                title={dictionary.home.apiUnavailableTitle}
                tone="warning"
              />
            ) : (
              <div className={styles.cards}>
                {(repositories ?? []).map((repository) => (
                  <RepositoryCard dictionary={dictionary} key={repository.id} locale={locale} repository={repository} />
                ))}
              </div>
            )}
          </section>
        </main>
      </div>
    </div>
  );
}
