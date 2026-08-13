"use client";

import {
  Archive,
  BarChart3,
  Bell,
  BookMarked,
  BookOpenText,
  CircleDot,
  Code2,
  Ellipsis,
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
import { useEffect, useRef, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Repository, RepositoryEngagement } from "@/lib/api-types";
import { deleteJson, putJson } from "@/lib/auth-client";
import { abbreviateCount } from "@/lib/format";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { brandedAuthUrl, localizedPath, repositoryPath } from "@/lib/routes";

import styles from "./repository-header.module.css";

type RepositoryHeaderProps = {
  repository: Repository;
  locale: Locale;
  dictionary: Dictionary;
  session: AuthSession;
};

const tabs = [
  ["code", Code2],
  ["issues", CircleDot],
  ["pulls", GitPullRequest],
  ["discussions", MessageSquare],
  ["actions", PlayCircle],
  ["projects", Workflow],
  ["wiki", BookOpenText],
  ["security", ShieldCheck],
  ["insights", BarChart3],
] as const;

const moreTabs = [
  ["locks", LockKeyhole],
  ["releases", Tags],
] as const;

const codeSubroutes = new Set(["blob", "commits", "commit", "branches", "compare", "tags"]);

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
        <BookMarked aria-hidden="true" className={styles.icon} size={17} />
        <div className={styles.summaryDetails}>
          <div className={styles.path}>
            <Link href={localizedPath(locale, repository.owner)}>{repository.owner}</Link>
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
      {message && (
        <p className={styles.engagementMessage} role="status">
          {message}
        </p>
      )}
      {repository.archivedAt && (
        <div className={styles.archiveBanner} role="status">
          <Archive aria-hidden="true" size={16} />
          {dictionary.repositoryLifecycle.banner}
        </div>
      )}
      <nav aria-label={dictionary.common.repositoryNavigation} className={styles.navigation}>
        {tabs.map(([section, Icon]) => {
          const href = repositoryPath(locale, repository.owner, repository.slug, section);
          const active = isSectionActive(pathname, basePath, section);
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
              {count !== null && count > 0 && <span>{count}</span>}
            </Link>
          );
        })}
        <RepositoryMoreMenu
          basePath={basePath}
          dictionary={dictionary}
          locale={locale}
          pathname={pathname}
          repository={repository}
        />
        {session.status === "authenticated" && (
          <RepositoryTab
            active={isSectionActive(pathname, basePath, "settings")}
            href={repositoryPath(locale, repository.owner, repository.slug, "settings")}
            icon={Settings}
            label={dictionary.common.settings}
          />
        )}
      </nav>
    </div>
  );
}

function isSectionActive(pathname: string, basePath: string, section: string): boolean {
  const segment = sectionSegment(pathname, basePath);
  if (section === "code") {
    return segment === "" || codeSubroutes.has(segment);
  }
  return segment === section;
}

function sectionSegment(pathname: string, basePath: string): string {
  if (pathname === basePath) return "";
  const prefix = `${basePath}/`;
  if (!pathname.startsWith(prefix)) return "";
  return pathname.slice(prefix.length).split("/")[0] ?? "";
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
      Icon: Bell,
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
            <strong>{abbreviateCount(action.count, locale)}</strong>
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

function RepositoryMoreMenu({
  basePath,
  dictionary,
  locale,
  pathname,
  repository,
}: {
  basePath: string;
  dictionary: Dictionary;
  locale: Locale;
  pathname: string;
  repository: Repository;
}) {
  const active = moreTabs.some(([section]) => sectionSegment(pathname, basePath) === section);
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDetailsElement>(null);

  useEffect(() => {
    if (!open) return;
    function onClick(event: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setOpen(false);
      }
    }
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") setOpen(false);
    }
    document.addEventListener("click", onClick);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("click", onClick);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return (
    <details
      className={styles.moreMenu}
      onToggle={(event) => setOpen(event.currentTarget.open)}
      open={open}
      ref={containerRef}
    >
      <summary className={active ? styles.active : ""}>
        <Ellipsis aria-hidden="true" size={16} />
        {dictionary.common.more}
      </summary>
      <div className={styles.moreDropdown}>
        {moreTabs.map(([section, Icon]) => (
          <Link
            href={repositoryPath(locale, repository.owner, repository.slug, section)}
            key={section}
            onClick={() => setOpen(false)}
          >
            <Icon aria-hidden="true" size={16} />
            {dictionary.common[section]}
          </Link>
        ))}
      </div>
    </details>
  );
}

function RepositoryTab({
  active,
  href,
  icon: Icon,
  label,
}: {
  active: boolean;
  href: string;
  icon: typeof Settings;
  label: string;
}) {
  return (
    <Link aria-current={active ? "page" : undefined} className={active ? styles.active : ""} href={href}>
      <Icon aria-hidden="true" size={16} />
      {label}
    </Link>
  );
}
