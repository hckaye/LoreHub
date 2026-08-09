import { Box, CircleDot, GitBranch, GitPullRequest, PlayCircle } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { Repository } from "@/lib/api-types";

import styles from "./repository-header.module.css";

type RepositoryHeaderProps = {
  repository: Repository;
  locale: Locale;
  dictionary: Dictionary;
};

export function RepositoryHeader({ repository, locale, dictionary }: RepositoryHeaderProps) {
  const basePath = `/${locale}/${repository.owner}/${repository.slug}`;
  return (
    <div className={styles.wrapper}>
      <div className={styles.summary}>
        <Box aria-hidden="true" className={styles.icon} size={24} />
        <div>
          <div className={styles.path}>
            <Link href={`/${locale}`}>{repository.owner}</Link>
            <span>/</span>
            <strong>{repository.slug}</strong>
            <span className={styles.visibility}>{dictionary.common[repository.visibility]}</span>
          </div>
          <p>{repository.description || dictionary.repository.noDescription}</p>
        </div>
      </div>
      <nav aria-label={dictionary.repository.navigationLabel} className={styles.navigation}>
        <Link className={styles.active} href={basePath}>
          <Box aria-hidden="true" size={16} />
          {dictionary.repository.overview}
        </Link>
        <Link href={`${basePath}#issues`}>
          <CircleDot aria-hidden="true" size={16} />
          {dictionary.common.issues}
          <span>{repository.issueCount}</span>
        </Link>
        <Link href={`${basePath}#branches`}>
          <GitBranch aria-hidden="true" size={16} />
          {dictionary.common.branches}
        </Link>
        <Link href={`${basePath}#reviews`}>
          <GitPullRequest aria-hidden="true" size={16} />
          {dictionary.common.reviews}
          <span>{repository.mergeRequestCount}</span>
        </Link>
        <Link href={`${basePath}#actions`}>
          <PlayCircle aria-hidden="true" size={16} />
          {dictionary.common.actions}
        </Link>
      </nav>
    </div>
  );
}
