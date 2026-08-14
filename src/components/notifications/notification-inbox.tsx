"use client";

import type { LucideIcon } from "lucide-react";
import { Bell, Check, CircleDot, GitCommit, GitPullRequest, Inbox, List, MessageSquare, Tag } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Notification } from "@/lib/api-types";
import { patchJson, postJson } from "@/lib/auth-client";
import { formatEventTopic } from "@/lib/event-topic-label";
import { formatRelativeTime } from "@/lib/format";
import { localizeHref } from "@/lib/routes";

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
  const unreadCount = items.filter((item) => !item.readAt).length;

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
    <div className={styles.layout}>
      <nav aria-label={dictionary.notificationsPage.title} className={styles.sidebar}>
        <ul className={styles.nav}>
          <li>
            <button
              aria-current={filter === "unread" ? "page" : undefined}
              className={styles.navItem}
              onClick={() => setFilter("unread")}
              type="button"
            >
              <Inbox aria-hidden="true" className={styles.navIcon} size={16} />
              <span className={styles.navLabel}>{dictionary.notificationsPage.inbox}</span>
              {unreadCount > 0 && (
                <span className={styles.counter}>
                  {unreadCount}
                  <span className={styles.visuallyHidden}> {dictionary.notificationsPage.unreadCountLabel}</span>
                </span>
              )}
            </button>
          </li>
          <li>
            <button
              aria-current={filter === "all" ? "page" : undefined}
              className={styles.navItem}
              onClick={() => setFilter("all")}
              type="button"
            >
              <List aria-hidden="true" className={styles.navIcon} size={16} />
              <span className={styles.navLabel}>{dictionary.notificationsPage.filterAll}</span>
            </button>
          </li>
        </ul>
      </nav>
      <div className={styles.main}>
        <div className={styles.box}>
          <div className={styles.boxHeader}>
            <button className={styles.markAllButton} disabled={unreadCount === 0} onClick={markAllRead} type="button">
              <Check aria-hidden="true" size={16} />
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
                const unread = !item.readAt;
                const source = repo ?? formatEventTopic(dictionary.eventTopics, item.topic);
                return (
                  <li className={styles.row} data-unread={unread} key={item.id}>
                    <span aria-hidden="true" className={styles.dot} />
                    <Icon aria-hidden="true" className={styles.rowIcon} size={16} />
                    <Link
                      className={styles.rowLink}
                      href={localizeHref(item.href, locale)}
                      onClick={() => unread && void markRead(item)}
                    >
                      {source && <span className={styles.source}>{source}</span>}
                      <span className={styles.rowTitle}>{item.title}</span>
                    </Link>
                    <span className={styles.rowMeta}>
                      <time className={styles.time} dateTime={item.createdAt}>
                        {formatRelativeTime(item.createdAt, locale)}
                      </time>
                      <span className={styles.rowActions}>
                        {unread && (
                          <button
                            aria-label={dictionary.notificationsPage.markRead}
                            className={styles.rowAction}
                            onClick={() => void markRead(item)}
                            title={dictionary.notificationsPage.markRead}
                            type="button"
                          >
                            <Check aria-hidden="true" size={16} />
                          </button>
                        )}
                      </span>
                    </span>
                  </li>
                );
              })}
            </ul>
          ) : (
            <div className={styles.blankslate}>
              <Bell aria-hidden="true" className={styles.blankslateIcon} size={24} />
              <h2 className={styles.blankslateTitle}>
                {filter === "unread" && items.length > 0
                  ? dictionary.notificationsPage.caughtUpTitle
                  : dictionary.notificationsPage.emptyTitle}
              </h2>
              <p className={styles.blankslateBody}>
                {filter === "unread" && items.length > 0
                  ? dictionary.notificationsPage.caughtUpBody
                  : dictionary.notificationsPage.emptyBody}
              </p>
            </div>
          )}
        </div>
      </div>
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
