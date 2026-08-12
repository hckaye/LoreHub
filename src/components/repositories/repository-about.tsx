import { Bell, ExternalLink, Star } from "lucide-react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { Repository } from "@/lib/api-types";

import styles from "./repository-about.module.css";
import { RepositoryTopicList } from "./repository-topic-list";

type RepositoryAboutProps = {
  dictionary: Dictionary;
  locale: Locale;
  repository: Repository;
};

export function RepositoryAbout({ dictionary, locale, repository }: RepositoryAboutProps) {
  return (
    <aside aria-labelledby="repository-about-title" className={styles.about}>
      <h2 id="repository-about-title">{dictionary.repository.about}</h2>
      <p className={styles.description}>{repository.description || dictionary.common.noDescription}</p>
      {repository.homepageUrl && (
        <a className={styles.homepage} href={repository.homepageUrl} rel="noreferrer" target="_blank">
          <ExternalLink aria-hidden="true" size={16} />
          <span>{repository.homepageUrl}</span>
        </a>
      )}
      <RepositoryTopicList
        className={styles.topics}
        label={dictionary.settingsPage.topics}
        locale={locale}
        topics={repository.topics}
      />
      <ul className={styles.stats}>
        <li>
          <Star aria-hidden="true" size={16} />
          <strong>{(repository.starCount ?? 0).toLocaleString(locale)}</strong>
          {dictionary.repository.stars}
        </li>
        <li>
          <Bell aria-hidden="true" size={16} />
          <strong>{(repository.watcherCount ?? 0).toLocaleString(locale)}</strong>
          {dictionary.repository.watching}
        </li>
      </ul>
    </aside>
  );
}
