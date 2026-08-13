"use client";

import Link from "next/link";
import { useSyncExternalStore, type ReactNode } from "react";

import type { Locale } from "@/i18n/config";
import { formatDate, formatRelativeTime } from "@/lib/format";
import { localizedPath } from "@/lib/routes";

import { UserAvatar } from "../ui/user-avatar";
import styles from "./issue-detail.module.css";

type TimelineItemProps = {
  actions?: ReactNode;
  author: string;
  children: ReactNode;
  locale: Locale;
  meta?: ReactNode;
  template: string;
  timestamp: string;
};

export function TimelineItem(props: TimelineItemProps) {
  const profile = localizedPath(props.locale, props.author);
  return (
    <div className={styles.timelineItem}>
      <Link aria-hidden="true" className={styles.gutter} href={profile} tabIndex={-1}>
        <UserAvatar name={props.author} size={40} />
      </Link>
      <article className={styles.card}>
        <header className={styles.cardHeader}>
          <Link className={styles.author} href={profile}>
            {props.author}
          </Link>
          <RelativeTime locale={props.locale} template={props.template} value={props.timestamp} />
          {props.meta}
          {props.actions && <span className={styles.cardActions}>{props.actions}</span>}
        </header>
        {props.children}
      </article>
    </div>
  );
}

type RelativeTimeProps = {
  locale: Locale;
  template?: string;
  value: string;
};

// The server and the browser evaluate "now" at different moments, so the absolute date is
// rendered until hydration finishes and the relative label can be computed client-side.
export function RelativeTime({ locale, template, value }: RelativeTimeProps) {
  const hydrated = useSyncExternalStore(
    subscribeToNothing,
    () => true,
    () => false,
  );
  const absolute = formatDate(value, locale);
  const label = hydrated ? formatRelativeTime(value, locale) : absolute;
  return (
    <time dateTime={value} title={absolute}>
      {template ? template.replace("{time}", label) : label}
    </time>
  );
}

function subscribeToNothing(): () => void {
  return () => {};
}
