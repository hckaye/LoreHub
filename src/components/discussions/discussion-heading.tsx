"use client";

import { CircleDot, LockKeyhole, Pin, Unlock, XCircle } from "lucide-react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Discussion } from "@/lib/api-types";
import { formatTimestamp } from "@/lib/format";

import styles from "./discussion-detail.module.css";

export type DiscussionHeadingProps = {
  busy: string | null;
  canChangeState: boolean;
  copy: Dictionary["discussionsPage"];
  discussion: Discussion;
  locale: Locale;
  onDelete: () => Promise<void>;
  onEdit: () => void;
  onToggleLock: () => Promise<boolean>;
  onTogglePin: () => Promise<boolean>;
  onToggleState: () => Promise<boolean>;
  onToggleVote: () => Promise<void>;
  session: AuthSession;
};

export function DiscussionHeading(props: DiscussionHeadingProps) {
  return (
    <header className={styles.heading}>
      <div>
        <h1>
          {props.discussion.title} <span className={styles.number}>#{props.discussion.number}</span>
        </h1>
        <div className={styles.meta}>
          <span className={styles.badge} data-state={props.discussion.state}>
            {props.discussion.state === "open" ? <CircleDot size={13} /> : <XCircle size={13} />}
            {props.discussion.state === "open" ? props.copy.open : props.copy.closed}
          </span>
          <span>{props.discussion.category.name}</span>
          <span>
            {props.discussion.author.displayName} · {formatTimestamp(props.discussion.createdAt, props.locale)}
          </span>
        </div>
      </div>
      <div className={`${styles.actions} ${styles.headingActions}`}>
        {props.session.status === "authenticated" && props.discussion.viewerCanVote ? (
          <button
            className={styles.secondaryButton}
            disabled={props.busy !== null}
            onClick={() => void props.onToggleVote()}
            type="button"
          >
            {props.discussion.viewerHasVoted ? props.copy.voted : props.copy.vote} ({props.discussion.voteCount})
          </button>
        ) : (
          <span className={styles.voteCount}>
            {props.copy.vote} ({props.discussion.voteCount})
          </span>
        )}
        {props.discussion.viewerCanEdit && (
          <>
            <button className={styles.secondaryButton} onClick={props.onEdit} type="button">
              {props.copy.edit}
            </button>
            <button
              className={styles.dangerButton}
              disabled={props.busy !== null}
              onClick={() => void props.onDelete()}
              type="button"
            >
              {props.copy.delete}
            </button>
          </>
        )}
        {props.canChangeState && (
          <button
            className={styles.secondaryButton}
            disabled={props.busy !== null}
            onClick={() => void props.onToggleState()}
            type="button"
          >
            {props.discussion.state === "open" ? props.copy.close : props.copy.reopen}
          </button>
        )}
        {props.discussion.viewerCanModerate && (
          <>
            <button
              className={styles.secondaryButton}
              disabled={props.busy !== null}
              onClick={() => void props.onToggleLock()}
              type="button"
            >
              {props.discussion.locked ? <Unlock size={14} /> : <LockKeyhole size={14} />}
              {props.discussion.locked ? props.copy.unlock : props.copy.lock}
            </button>
            <button
              className={styles.secondaryButton}
              disabled={props.busy !== null}
              onClick={() => void props.onTogglePin()}
              type="button"
            >
              <Pin size={14} /> {props.discussion.pinned ? props.copy.unpin : props.copy.pin}
            </button>
          </>
        )}
      </div>
    </header>
  );
}
