"use client";

import { Bell, Check } from "lucide-react";
import Link from "next/link";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, Notification } from "@/lib/api-types";
import { patchJson, postJson } from "@/lib/auth-client";

import { EmptyState } from "../ui/empty-state";
import styles from "./notification-inbox.module.css";

type NotificationInboxProps = {
  dictionary: Dictionary;
  initialItems: Notification[];
  locale: "en" | "ja";
  session: Extract<AuthSession, { status: "authenticated" }>;
};

export function NotificationInbox({ dictionary, initialItems, locale, session }: NotificationInboxProps) {
  const [items, setItems] = useState(initialItems);
  const [unreadOnly, setUnreadOnly] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const visibleItems = unreadOnly ? items.filter((item) => !item.readAt) : items;

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
    } else {
      setStatus(dictionary.notificationsPage.unavailableTitle);
    }
  }

  async function markAllRead() {
    const result = await postJson<{ read: boolean }>("/api/v1/notifications/read-all", {}, session.csrfToken);
    if (result.ok) {
      const timestamp = new Date().toISOString();
      setItems((current) => current.map((item) => ({ ...item, readAt: item.readAt ?? timestamp })));
    } else {
      setStatus(dictionary.notificationsPage.unavailableTitle);
    }
  }

  return (
    <div className={styles.wrapper}>
      <div className={styles.toolbar}>
        <label className={styles.filter}>
          <input checked={unreadOnly} onChange={(event) => setUnreadOnly(event.target.checked)} type="checkbox" />
          {dictionary.notificationsPage.unreadOnly}
        </label>
        <button disabled={!items.some((item) => !item.readAt)} onClick={markAllRead} type="button">
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
          {visibleItems.map((item) => (
            <li data-read={Boolean(item.readAt)} key={item.id}>
              <Link href={localizeHref(item.href, locale)} onClick={() => !item.readAt && void markRead(item)}>
                <span className={styles.icon} aria-hidden="true">
                  <Bell size={17} />
                </span>
                <span className={styles.content}>
                  <strong>{item.title}</strong>
                  <span>{item.body || item.topic}</span>
                  <time dateTime={item.createdAt}>{formatDate(item.createdAt, locale)}</time>
                </span>
              </Link>
              {!item.readAt && (
                <button className={styles.readButton} onClick={() => void markRead(item)} type="button">
                  {dictionary.notificationsPage.markRead}
                </button>
              )}
            </li>
          ))}
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

function localizeHref(href: string, locale: "en" | "ja"): string {
  if (href === "/" || href.startsWith(`/${locale}/`)) {
    return href === "/" ? `/${locale}` : href;
  }
  return `/${locale}${href.startsWith("/") ? href : `/${href}`}`;
}

function formatDate(value: string, locale: "en" | "ja"): string {
  return new Intl.DateTimeFormat(locale === "ja" ? "ja-JP" : "en-US", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}
