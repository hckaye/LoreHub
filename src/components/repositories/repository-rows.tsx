"use client";

import { BookMarked } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { Repository } from "@/lib/api-types";
import { formatRelativeTime } from "@/lib/format";
import { repositoryPath } from "@/lib/routes";

import { EmptyState } from "../ui/empty-state";
import styles from "./repository-rows.module.css";

type RepositoryRowsProps = {
  dictionary: Dictionary;
  emptyBody?: string;
  emptyTitle?: string;
  locale: Locale;
  repositories: Repository[] | null;
  unavailable?: boolean;
};

export function RepositoryRows({
  dictionary,
  emptyBody,
  emptyTitle,
  locale,
  repositories,
  unavailable = false,
}: RepositoryRowsProps) {
  const [query, setQuery] = useState("");
  const filtered = useMemo(() => {
    const list = repositories ?? [];
    const normalized = query.trim().toLocaleLowerCase();
    if (!normalized) {
      return list;
    }
    return list.filter((repository) =>
      [repository.owner, repository.slug, repository.displayName, repository.description, ...repository.topics]
        .join(" ")
        .toLocaleLowerCase()
        .includes(normalized),
    );
  }, [repositories, query]);
  const emptyFromFilter = query.trim().length > 0;

  return (
    <section aria-label={dictionary.profile.repositories}>
      <input
        aria-label={dictionary.profile.filterRepositories}
        className={styles.filter}
        onChange={(event) => setQuery(event.target.value)}
        placeholder={dictionary.profile.filterRepositories}
        type="search"
        value={query}
      />
      {filtered.length > 0 ? (
        <ul className={styles.list}>
          {filtered.map((repository) => (
            <RepositoryRow dictionary={dictionary} key={repository.id} locale={locale} repository={repository} />
          ))}
        </ul>
      ) : (
        <EmptyState
          body={
            unavailable
              ? dictionary.home.apiUnavailableBody
              : emptyFromFilter
                ? dictionary.home.searchEmptyBody
                : (emptyBody ?? dictionary.profile.repositoriesEmptyBody)
          }
          icon={<BookMarked aria-hidden="true" />}
          title={
            unavailable
              ? dictionary.home.apiUnavailableTitle
              : emptyFromFilter
                ? dictionary.home.searchEmptyTitle
                : (emptyTitle ?? dictionary.profile.repositoriesEmptyTitle)
          }
          tone={unavailable ? "warning" : "neutral"}
        />
      )}
    </section>
  );
}

type RepositoryRowProps = {
  dictionary: Dictionary;
  locale: Locale;
  repository: Repository;
};

function RepositoryRow({ dictionary, locale, repository }: RepositoryRowProps) {
  const href = repositoryPath(locale, repository.owner, repository.slug);
  const updated = repository.updatedAt
    ? dictionary.common.updatedOn.replace("{time}", formatRelativeTime(repository.updatedAt, locale))
    : "";
  return (
    <li className={styles.row}>
      <div className={styles.heading}>
        <h3>
          <Link href={href}>{repository.displayName}</Link>
        </h3>
        <span className={styles.visibility}>{dictionary.common[repository.visibility]}</span>
      </div>
      {repository.description ? <p className={styles.description}>{repository.description}</p> : null}
      {updated ? (
        <p className={styles.meta}>
          <time dateTime={repository.updatedAt}>{updated}</time>
        </p>
      ) : null}
    </li>
  );
}
