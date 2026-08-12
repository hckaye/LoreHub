import { CheckCircle2, CircleDot, MessageCircle, Pin, ThumbsUp } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { DiscussionSummary } from "@/lib/api-types";

import styles from "./discussion-list.module.css";

type DiscussionListItemProps = {
  dictionary: Dictionary;
  discussion: DiscussionSummary;
  href: string;
  locale: Locale;
};

export function DiscussionListItem(props: DiscussionListItemProps) {
  const copy = props.dictionary.discussionsPage;
  return (
    <article className={styles.item}>
      <div>
        <h2>
          <Link href={props.href}>{props.discussion.title}</Link>
          <span className={styles.number}>#{props.discussion.number}</span>
        </h2>
        <div className={styles.meta}>
          <span className={styles.badge} data-state={props.discussion.state}>
            {props.discussion.state === "open" ? (
              <CircleDot aria-hidden="true" size={13} />
            ) : (
              <CheckCircle2 size={13} />
            )}
            {props.discussion.state === "open" ? copy.open : copy.closed}
          </span>
          <span className={styles.category}>{props.discussion.category.name}</span>
          {props.discussion.pinned && (
            <span className={styles.badge}>
              <Pin aria-hidden="true" size={12} /> {copy.pinned}
            </span>
          )}
          {props.discussion.answered && (
            <span className={styles.badge}>
              <CheckCircle2 aria-hidden="true" size={12} /> {copy.answered}
            </span>
          )}
          <span>
            {props.discussion.author.displayName} · {formatDate(props.discussion.updatedAt, props.locale)}
          </span>
        </div>
      </div>
      <div className={styles.stats}>
        <span>
          <ThumbsUp aria-hidden="true" size={13} /> {props.discussion.voteCount}
        </span>
        <span>
          <MessageCircle aria-hidden="true" size={13} /> {props.discussion.commentCount}
        </span>
      </div>
    </article>
  );
}

function formatDate(value: string, locale: Locale): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeZone: "UTC" }).format(new Date(value));
}
