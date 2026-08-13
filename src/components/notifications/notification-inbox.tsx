"use client";

import type { LucideIcon } from "lucide-react";
import { Bell, Check, CircleDot, GitCommit, GitPullRequest, MessageSquare, Tag } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Notification } from "@/lib/api-types";
import { patchJson, postJson } from "@/lib/auth-client";
import { formatRelativeTime } from "@/lib/format";

import { EmptyState } from "../ui/empty-state";
import styles from "./notification-inbox.module.css";

type NotificationInboxProps = {
  dictionary: Dictionary;
  initialItems: Notification[];
  locale: Locale;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

type Filter = "all" | "unread";

export function NotificationInbox({ dictionary, initialItems, locale, session }: NotificationInboxProps) {
  const router = useRouter();
  const [items, setItems] = useState(initialItems);
  const [filter, setFilter] = useState<Filter>("all");
  const [status, setStatus] = useState<string | null>(null);
  const visibleItems = filter === "unread" ? items.filter((item) => !item.readAt) : items;

  async function markRead(item: Notification) {
    const result = await patchJson<{ read: boolean }>(
      `/api/v1/notifications/${encodeURIComponent(item.id)}/read`,
      {},
      session.csrfToken,
    );
    if (result.ok) {
      setItems((current) =>
        current.map((entry) => (entry.id === item.id ? { ...entry, readAt: new Date().toISOString() } : entry)),
      );
      setStatus(null);
      router.refresh();
    } else {
      setStatus(dictionary.notificationsPage.markReadFailed);
    }
  }

  async function markAllRead() {
    const result = await postJson<{ read: boolean }>("/api/v1/notifications/read-all", {}, session.csrfToken);
    if (result.ok) {
      const timestamp = new Date().toISOString();
      setItems((current) => current.map((item) => ({ ...item, readAt: item.readAt ?? timestamp })));
      setStatus(null);
      router.refresh();
    } else {
      setStatus(dictionary.notificationsPage.markReadFailed);
    }
  }

  return (
    <div className={styles.wrapper}>
      <div className={styles.toolbar}>
        <div className={styles.tabs} role="tablist">
          <button
            aria-current={filter === "all"}
            className={styles.tab}
            onClick={() => setFilter("all")}
            role="tab"
            type="button"
          >
            {dictionary.notificationsPage.filterAll}
          </button>
          <button
            aria-current={filter === "unread"}
            className={styles.tab}
            onClick={() => setFilter("unread")}
            role="tab"
            type="button"
          >
            {dictionary.notificationsPage.filterUnread}
          </button>
        </div>
        <button
          className={styles.markAllButton}
          disabled={!items.some((item) => !item.readAt)}
          onClick={markAllRead}
          type="button"
        >
          <Check aria-hidden="true" size={15} />
          {dictionary.notificationsPage.markAllRead}
        </button>
      </div>
      {status && (
        <p className={styles.error} role="alert">
          {status}
        </p>
      )}
      {visibleItems.length > 0 ? (
        <ul className={styles.list}>
          {visibleItems.map((item) => {
            const { icon, repo } = describeNotification(item.href);
            const Icon = icon;
            return (
              <li data-read={Boolean(item.readAt)} key={item.id}>
                <Link href={localizeHref(item.href, locale)} onClick={() => !item.readAt && void markRead(item)}>
                  <span className={styles.icon} aria-hidden="true">
                    <Icon size={17} />
                  </span>
                  <span className={styles.content}>
                    {repo && <span className={styles.repo}>{repo}</span>}
                    <strong>{item.title}</strong>
                    <span>{item.body || item.topic}</span>
                    <time dateTime={item.createdAt}>{formatRelativeTime(item.createdAt, locale)}</time>
                  </span>
                </Link>
                {!item.readAt && (
                  <button className={styles.readButton} onClick={() => void markRead(item)} type="button">
                    {dictionary.notificationsPage.markRead}
                  </button>
                )}
              </li>
            );
          })}
        </ul>
      ) : (
        <EmptyState
          body={dictionary.notificationsPage.emptyBody}
          icon={<Bell aria-hidden="true" />}
          title={dictionary.notificationsPage.emptyTitle}
        />
      )}
    </div>
  );
}

type NotificationDescriptor = { icon: LucideIcon; repo: string | null };

function describeNotification(href: string): NotificationDescriptor {
  const segments = href.split("?")[0].split("/").filter(Boolean);
  const repo = repoPrefixFromSegments(segments);
  const path = href.split("?")[0];
  if (path.includes("/issues/")) return { icon: CircleDot, repo };
  if (path.includes("/pulls/")) return { icon: GitPullRequest, repo };
  if (path.includes("/discussions/")) return { icon: MessageSquare, repo };
  if (path.includes("/releases")) return { icon: Tag, repo };
  if (path.includes("/commit")) return { icon: GitCommit, repo };
  return { icon: Bell, repo };
}

function repoPrefixFromSegments(segments: string[]): string | null {
  if (segments.length < 2) return null;
  const [owner, repo] = segments;
  if (owner === "organizations" || owner === "settings" || owner === "notifications") {
    return null;
  }
  return `${owner}/${repo}`;
}

function localizeHref(href: string, locale: Locale): string {
  if (href === "/" || href.startsWith(`/${locale}/`)) {
    return href === "/" ? `/${locale}` : href;
  }
  return `/${locale}${href.startsWith("/") ? href : `/${href}`}`;
}
