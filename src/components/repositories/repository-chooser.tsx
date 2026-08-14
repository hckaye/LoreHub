"use client";

import { BookMarked, ServerOff } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";

import styles from "@/components/create/create-form.module.css";
import { EmptyState } from "@/components/ui/empty-state";
import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { Repository } from "@/lib/api-types";
import { repositoryPath } from "@/lib/routes";

type RepositoryChooserProps = {
  locale: Locale;
  dictionary: Dictionary;
  repositories: Repository[] | null;
  unavailable: boolean;
  section: "issues" | "pulls";
};

export function RepositoryChooser({ locale, dictionary, repositories, unavailable, section }: RepositoryChooserProps) {
  const copy = dictionary.createPages;
  const [query, setQuery] = useState("");
  const writableRepositories = useMemo(
    () => (repositories ?? []).filter((repository) => !repository.archivedAt),
    [repositories],
  );
  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return writableRepositories;
    return writableRepositories.filter((repository) => {
      const haystack = `${repository.owner}/${repository.slug} ${repository.displayName}`.toLowerCase();
      return haystack.includes(needle);
    });
  }, [query, writableRepositories]);

  if (unavailable) {
    return (
      <EmptyState
        body={dictionary.home.apiUnavailableBody}
        icon={<ServerOff aria-hidden="true" />}
        title={dictionary.home.apiUnavailableTitle}
        tone="warning"
      />
    );
  }
  if (writableRepositories.length === 0) {
    return (
      <EmptyState
        body={dictionary.forms.chooseRepositoryEmpty}
        icon={<BookMarked aria-hidden="true" />}
        title={dictionary.forms.chooseRepositoryEmpty}
      />
    );
  }
  return (
    <div>
      <label className="visually-hidden" htmlFor="choose-repository">
        {copy.findARepository}
      </label>
      <input
        className={styles.search}
        id="choose-repository"
        onChange={(event) => setQuery(event.target.value)}
        placeholder={copy.findARepository}
        type="search"
        value={query}
      />
      {filtered.length === 0 ? (
        <p className={styles.hint}>{dictionary.forms.chooseRepositoryEmpty}</p>
      ) : (
        <ul className={styles.repoList}>
          {filtered.map((repository) => {
            const href = `${repositoryPath(locale, repository.owner, repository.slug, section)}/new`;
            return (
              <li key={repository.id}>
                <Link href={href}>
                  <BookMarked aria-hidden="true" size={16} />
                  <strong>
                    {repository.owner}/{repository.slug}
                  </strong>
                  {repository.displayName && repository.displayName !== repository.slug ? (
                    <span>{repository.displayName}</span>
                  ) : null}
                </Link>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
