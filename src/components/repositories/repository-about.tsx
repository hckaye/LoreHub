import { ExternalLink, Eye, Star, Tag } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { ReleasePage, Repository } from "@/lib/api-types";
import { abbreviateCount, formatRelativeTime } from "@/lib/format";
import { repositoryPath } from "@/lib/routes";

import styles from "./repository-about.module.css";
import { RepositoryTopicList } from "./repository-topic-list";

type RepositoryAboutProps = {
  dictionary: Dictionary;
  locale: Locale;
  owner: string;
  repository: Repository;
  repositorySlug: string;
  releasePage?: ReleasePage;
};

export function RepositoryAbout({
  dictionary,
  locale,
  owner,
  repository,
  repositorySlug,
  releasePage,
}: RepositoryAboutProps) {
  const description = repository.description.trim();
  const publishedReleases = releasePage?.releases.filter((release) => release.state === "published") ?? [];
  const latestRelease = publishedReleases[0];
  const releasesHref = repositoryPath(locale, owner, repositorySlug, "releases");
  const additionalReleaseCount = Math.max(0, publishedReleases.length - 1);

  return (
    <aside aria-labelledby="repository-about-title" className={styles.about}>
      <section className={styles.block}>
        <h2 id="repository-about-title">{dictionary.repository.about}</h2>
        {description && <p className={styles.description}>{description}</p>}
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
            <strong>{abbreviateCount(repository.starCount ?? 0, locale)}</strong>
            <span>{dictionary.repository.stars}</span>
          </li>
          <li>
            <Eye aria-hidden="true" size={16} />
            <strong>{abbreviateCount(repository.watcherCount ?? 0, locale)}</strong>
            <span>{dictionary.repository.watching}</span>
          </li>
        </ul>
      </section>
      {latestRelease && (
        <section aria-labelledby="repository-releases-title" className={styles.block}>
          <h2 id="repository-releases-title">{dictionary.releasesPage.title}</h2>
          <div className={styles.releaseItem}>
            <Tag aria-hidden="true" className={styles.releaseIcon} size={16} />
            <div className={styles.releaseDetails}>
              <Link className={styles.releaseTitle} href={`${releasesHref}#release-${latestRelease.tagName}`}>
                {latestRelease.title}
              </Link>
              <div className={styles.releaseMeta}>
                <span className={styles.latest}>{dictionary.releasesPage.latest}</span>
                <time dateTime={latestRelease.publishedAt ?? latestRelease.createdAt}>
                  {formatRelativeTime(latestRelease.publishedAt ?? latestRelease.createdAt, locale)}
                </time>
              </div>
            </div>
          </div>
          {additionalReleaseCount > 0 && (
            <Link className={styles.releaseMore} href={releasesHref}>
              {dictionary.repository.moreReleases.replace("{count}", abbreviateCount(additionalReleaseCount, locale))}
            </Link>
          )}
        </section>
      )}
    </aside>
  );
}
