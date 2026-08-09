import { CircleDot, GitPullRequest, LockKeyhole, PackageOpen } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { Repository } from "@/lib/api-types";

import styles from "./repository-card.module.css";

type RepositoryCardProps = {
  repository: Repository;
  locale: Locale;
  dictionary: Dictionary;
};

export function RepositoryCard({ repository, locale, dictionary }: RepositoryCardProps) {
  const visibilityLabel = dictionary.common[repository.visibility];
  return (
    <article className={styles.card}>
      <div className={styles.heading}>
        <div className={styles.repositoryIcon}>
          <PackageOpen aria-hidden="true" size={19} />
        </div>
        <div>
          <p className={styles.owner}>{repository.owner}</p>
          <h3>
            <Link href={`/${locale}/${repository.owner}/${repository.slug}`}>{repository.displayName}</Link>
          </h3>
        </div>
        <span className={styles.visibility}>
          {repository.visibility !== "public" && <LockKeyhole aria-hidden="true" size={12} />}
          {visibilityLabel}
        </span>
      </div>
      <p className={styles.description}>{repository.description || dictionary.repository.noDescription}</p>
      <div className={styles.meta}>
        <span>
          <CircleDot aria-hidden="true" size={15} />
          {repository.issueCount} {dictionary.common.issues}
        </span>
        <span>
          <GitPullRequest aria-hidden="true" size={15} />
          {repository.mergeRequestCount} {dictionary.common.reviews}
        </span>
        <code>{repository.defaultBranch}</code>
      </div>
    </article>
  );
}
