import { BookOpenText, Search, ServerOff } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { Repository } from "@/lib/api-types";

import { RepositoryGrid } from "../repositories/repository-grid";
import { EmptyState } from "../ui/empty-state";
import styles from "./public-explore.module.css";

type PublicExploreProps = {
  locale: Locale;
  dictionary: Dictionary;
  repositories: Repository[] | null;
  query: string;
  unavailable: boolean;
};

export function PublicExplore({ locale, dictionary, repositories, query, unavailable }: PublicExploreProps) {
  const hasQuery = Boolean(query.trim());
  return (
    <div className={styles.page}>
      <section className={styles.hero}>
        <div>
          <p className={styles.eyebrow}>{dictionary.home.eyebrow}</p>
          <h1>{dictionary.home.title}</h1>
          <p>{dictionary.home.intro}</p>
        </div>
        <Link
          className={styles.architectureLink}
          href="https://github.com/EpicGames/lore"
          rel="noreferrer"
          target="_blank"
        >
          <BookOpenText aria-hidden="true" size={17} />
          {dictionary.home.architecture}
        </Link>
      </section>
      <section aria-labelledby="explore-title" className={styles.repositories}>
        <div className={styles.sectionHeading}>
          <div>
            <h2 id="explore-title">{hasQuery ? dictionary.home.searchResults : dictionary.home.repositories}</h2>
            <p>{hasQuery ? dictionary.home.searchResultsDescription : dictionary.home.repositoriesDescription}</p>
          </div>
          <Search aria-hidden="true" className={styles.headingIcon} size={19} />
        </div>
        {unavailable ? (
          <EmptyState
            body={dictionary.home.apiUnavailableBody}
            icon={<ServerOff aria-hidden="true" />}
            title={dictionary.home.apiUnavailableTitle}
            tone="warning"
          />
        ) : (
          <RepositoryGrid
            dictionary={dictionary}
            emptyBody={hasQuery ? dictionary.home.searchEmptyBody : dictionary.home.emptyBody}
            emptyTitle={hasQuery ? dictionary.home.searchEmptyTitle : dictionary.home.emptyTitle}
            locale={locale}
            repositories={repositories ?? []}
          />
        )}
      </section>
    </div>
  );
}
