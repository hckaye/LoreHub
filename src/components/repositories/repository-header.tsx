"use client";

import {
  Archive,
  BarChart3,
  BookOpenText,
  Box,
  CircleDot,
  Eye,
  GitPullRequest,
  LockKeyhole,
  MessageSquare,
  PlayCircle,
  Settings,
  ShieldCheck,
  Star,
  Tags,
  Workflow,
} from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Repository, RepositoryEngagement } from "@/lib/api-types";
import { deleteJson, putJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { brandedAuthUrl, repositoryPath } from "@/lib/routes";

import styles from "./repository-header.module.css";
import { RepositoryTopicList } from "./repository-topic-list";

type RepositoryHeaderProps = {
  repository: Repository;
  locale: Locale;
  dictionary: Dictionary;
  session: AuthSession;
};

const tabs = [
  ["code", Box],
  ["locks", LockKeyhole],
  ["issues", CircleDot],
  ["discussions", MessageSquare],
  ["pulls", GitPullRequest],
  ["actions", PlayCircle],
  ["projects", Workflow],
  ["wiki", BookOpenText],
  ["releases", Tags],
  ["security", ShieldCheck],
  ["insights", BarChart3],
  ["settings", Settings],
] as const;

export function RepositoryHeader({ repository, locale, dictionary, session }: RepositoryHeaderProps) {
  const router = useRouter();
  const pathname = usePathname() ?? repositoryPath(locale, repository.owner, repository.slug);
  const basePath = repositoryPath(locale, repository.owner, repository.slug);
  const [engagement, setEngagement] = useState<RepositoryEngagement>({
    starCount: repository.starCount ?? 0,
    watcherCount: repository.watcherCount ?? 0,
    viewerHasStarred: repository.viewerHasStarred ?? false,
    viewerIsWatching: repository.viewerIsWatching ?? false,
  });
  const [busy, setBusy] = useState<"star" | "watch" | null>(null);
  const [message, setMessage] = useState("");

  async function mutateEngagement(kind: "star" | "watch", enabled: boolean) {
    if (session.status !== "authenticated") return;
    setBusy(kind);
    setMessage("");
    const path = `/api/v1/repositories/${encodeURIComponent(repository.owner)}/${encodeURIComponent(
      repository.slug,
    )}/${kind}`;
    const result = enabled
      ? await putJson<RepositoryEngagement>(path, {}, session.csrfToken)
      : await deleteJson<RepositoryEngagement>(path, session.csrfToken);
    setBusy(null);
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, dictionary));
      return;
    }
    setEngagement(result.data);
    router.refresh();
  }

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
            {repository.archivedAt && (
              <span className={styles.archived}>
                <Archive aria-hidden="true" size={12} />
                {dictionary.repositoryLifecycle.badge}
              </span>
            )}
          </div>
          <p>{repository.description || dictionary.common.noDescription}</p>
          <RepositoryTopicList
            className={styles.topics}
            label={dictionary.settingsPage.topics}
            locale={locale}
            topics={repository.topics}
          />
        </div>
        <RepositoryEngagementActions
          basePath={basePath}
          busy={busy}
          dictionary={dictionary}
          engagement={engagement}
          locale={locale}
          mutate={mutateEngagement}
          session={session}
        />
      </div>
      <p className={styles.engagementMessage} role="status">
        {message}
      </p>
      {repository.archivedAt && (
        <div className={styles.archiveBanner} role="status">
          <Archive aria-hidden="true" size={16} />
          {dictionary.repositoryLifecycle.banner}
        </div>
      )}
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

function RepositoryEngagementActions({
  basePath,
  busy,
  dictionary,
  engagement,
  locale,
  mutate,
  session,
}: {
  basePath: string;
  busy: "star" | "watch" | null;
  dictionary: Dictionary;
  engagement: RepositoryEngagement;
  locale: Locale;
  mutate(kind: "star" | "watch", enabled: boolean): Promise<void>;
  session: AuthSession;
}) {
  const labels = dictionary.repository;
  const loginHref = brandedAuthUrl(locale, basePath);
  const actions = [
    {
      kind: "watch" as const,
      active: engagement.viewerIsWatching,
      count: engagement.watcherCount,
      activeLabel: labels.unwatch,
      inactiveLabel: labels.watch,
      Icon: Eye,
    },
    {
      kind: "star" as const,
      active: engagement.viewerHasStarred,
      count: engagement.starCount,
      activeLabel: labels.unstar,
      inactiveLabel: labels.star,
      Icon: Star,
    },
  ];
  return (
    <div className={styles.engagementActions}>
      {actions.map((action) => {
        const label = action.active ? action.activeLabel : action.inactiveLabel;
        const content = (
          <>
            <action.Icon aria-hidden="true" fill={action.active ? "currentColor" : "none"} size={16} />
            <span>{label}</span>
            <strong>{action.count}</strong>
          </>
        );
        if (session.status === "authenticated") {
          return (
            <button
              aria-pressed={action.active}
              disabled={busy !== null}
              key={action.kind}
              onClick={() => mutate(action.kind, !action.active)}
              type="button"
            >
              {content}
            </button>
          );
        }
        if (session.status === "unavailable") {
          return (
            <button disabled key={action.kind} type="button">
              {content}
            </button>
          );
        }
        return (
          <Link aria-label={label} href={loginHref} key={action.kind} role="button">
            {content}
          </Link>
        );
      })}
    </div>
  );
}
