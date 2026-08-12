import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { SearchRepository } from "@/lib/search";

import { RepositoryCard } from "../repositories/repository-card";
import styles from "./search-page.module.css";

type SearchRepositoryResultsProps = {
  count: number;
  dictionary: Dictionary;
  locale: Locale;
  repositories: SearchRepository[];
};

export function SearchRepositoryResults(props: SearchRepositoryResultsProps) {
  return (
    <section className={styles.section}>
      <h2>
        {props.dictionary.searchPage.repositories}
        <span>{props.count}</span>
      </h2>
      {props.repositories.length ? (
        <div className={styles.repositoryGrid}>
          {props.repositories.map((repository) => (
            <RepositoryCard
              dictionary={props.dictionary}
              key={repository.id}
              locale={props.locale}
              repository={repository}
            />
          ))}
        </div>
      ) : (
        <p className={styles.sectionEmpty}>{props.dictionary.searchPage.sectionEmpty}</p>
      )}
    </section>
  );
}
