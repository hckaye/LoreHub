"use client";

import {
  BarChart3,
  Box,
  CircleDot,
  GitPullRequest,
  LockKeyhole,
  PlayCircle,
  Settings,
  ShieldCheck,
  Workflow,
} from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { Repository } from "@/lib/api-types";
import { repositoryPath } from "@/lib/routes";

import styles from "./repository-header.module.css";

type RepositoryHeaderProps = {
  repository: Repository;
  locale: Locale;
  dictionary: Dictionary;
};

const tabs = [
  ["code", Box],
  ["issues", CircleDot],
  ["pulls", GitPullRequest],
  ["actions", PlayCircle],
  ["projects", Workflow],
  ["security", ShieldCheck],
  ["insights", BarChart3],
  ["settings", Settings],
] as const;

export function RepositoryHeader({ repository, locale, dictionary }: RepositoryHeaderProps) {
  const pathname = usePathname() ?? repositoryPath(locale, repository.owner, repository.slug);
  const basePath = repositoryPath(locale, repository.owner, repository.slug);
  return (
    <div className={styles.wrapper}>
      <div className={styles.summary}>
        <Box aria-hidden="true" className={styles.icon} size={25} />
        <div className={styles.summaryDetails}>
          <div className={styles.path}>
            <Link href={`/${locale}`}>{repository.owner}</Link>
            <span>/</span>
            <strong>{repository.slug}</strong>
            <span className={styles.visibility}>
              {repository.visibility !== "public" && <LockKeyhole aria-hidden="true" size={12} />}
              {dictionary.common[repository.visibility]}
            </span>
          </div>
          <p>{repository.description || dictionary.common.noDescription}</p>
        </div>
      </div>
      <nav aria-label={dictionary.common.repositoryNavigation} className={styles.navigation}>
        {tabs.map(([section, Icon]) => {
          const href = repositoryPath(locale, repository.owner, repository.slug, section);
          const active =
            section === "code" ? pathname === basePath || pathname === `${basePath}/` : pathname.startsWith(href);
          const label = dictionary.common[section === "pulls" ? "pullRequests" : section];
          const count =
            section === "issues" ? repository.issueCount : section === "pulls" ? repository.mergeRequestCount : null;
          return (
            <Link
              aria-current={active ? "page" : undefined}
              className={active ? styles.active : ""}
              href={href}
              key={section}
            >
              <Icon aria-hidden="true" size={16} />
              {label}
              {count !== null && <span>{count}</span>}
            </Link>
          );
        })}
      </nav>
    </div>
  );
}
