import { Building2, Search as SearchIcon, UserRound } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { SearchResults } from "@/lib/api-types";

import { RepositoryCard } from "../repositories/repository-card";
import { EmptyState } from "../ui/empty-state";
import styles from "./search-page.module.css";

type SearchPageProps = {
  dictionary: Dictionary;
  locale: Locale;
  query: string;
  results: SearchResults | null;
};

export function SearchPage({ dictionary, locale, query, results }: SearchPageProps) {
  const hasResults = Boolean(
    results && (results.repositories.length > 0 || results.organizations.length > 0 || results.users.length > 0),
  );
  return (
    <div className={styles.page}>
      <div className={styles.heading}>
        <p className={styles.eyebrow}>{dictionary.common.search}</p>
        <h1>{query ? `${dictionary.home.searchResults}: ${query}` : dictionary.home.discoverTitle}</h1>
        <p>{dictionary.home.searchResultsDescription}</p>
      </div>
      {!query ? (
        <EmptyState
          body={dictionary.home.searchEmptyBody}
          icon={<SearchIcon aria-hidden="true" />}
          title={dictionary.home.searchEmptyTitle}
        />
      ) : !results ? (
        <EmptyState
          body={dictionary.home.apiUnavailableBody}
          icon={<SearchIcon aria-hidden="true" />}
          title={dictionary.home.apiUnavailableTitle}
          tone="warning"
        />
      ) : !hasResults ? (
        <EmptyState
          body={dictionary.home.searchEmptyBody}
          icon={<SearchIcon aria-hidden="true" />}
          title={dictionary.home.searchEmptyTitle}
        />
      ) : (
        <div className={styles.results}>
          {results.repositories.length > 0 && (
            <section>
              <h2>{dictionary.common.repositories}</h2>
              <div className={styles.repositoryGrid}>
                {results.repositories.map((repository) => (
                  <RepositoryCard dictionary={dictionary} key={repository.id} locale={locale} repository={repository} />
                ))}
              </div>
            </section>
          )}
          {results.organizations.length > 0 && (
            <section>
              <h2>
                <Building2 aria-hidden="true" size={18} />
                {dictionary.common.organizations}
              </h2>
              <ul className={styles.list}>
                {results.organizations.map((organization) => (
                  <li key={organization.id}>
                    <Link href={`/${locale}/organizations/${encodeURIComponent(organization.slug)}`}>
                      <strong>{organization.displayName}</strong>
                      <span>
                        {organization.slug} · {organization.visibility}
                      </span>
                    </Link>
                  </li>
                ))}
              </ul>
            </section>
          )}
          {results.users.length > 0 && (
            <section>
              <h2>
                <UserRound aria-hidden="true" size={18} />
                {dictionary.profile.title}
              </h2>
              <ul className={styles.list}>
                {results.users.map((user) => (
                  <li key={user.id}>
                    <Link href={`/${locale}/${encodeURIComponent(user.username)}`}>
                      <strong>{user.displayName}</strong>
                      <span>@{user.username}</span>
                    </Link>
                  </li>
                ))}
              </ul>
            </section>
          )}
        </div>
      )}
    </div>
  );
}
