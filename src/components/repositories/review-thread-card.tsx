"use client";

import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { ReviewThread, ReviewThreadComment } from "@/lib/api-types";
import { formatDate, formatRelativeTime } from "@/lib/format";
import {
  deleteReviewComment,
  replyToReviewThread,
  setReviewThreadResolved,
  updateReviewComment,
} from "@/lib/review-thread-client";

import { UserAvatar } from "../ui/user-avatar";
import { MarkdownContent } from "../wiki/markdown-content";
import styles from "./review-diff-view.module.css";

export type ReviewThreadCardProps = {
  owner: string;
  repository: string;
  number: number;
  locale: Locale;
  csrfToken: string;
  authenticated: boolean;
  dictionary: Dictionary;
  thread: ReviewThread;
  updateThread: (thread: ReviewThread) => void;
  pendingReviewId: string | null;
  onPendingComment: () => void;
};

export function ReviewThreadCard(props: ReviewThreadCardProps) {
  const [reply, setReply] = useState("");
  const [editing, setEditing] = useState<string | null>(null);
  const [editBody, setEditBody] = useState("");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");

  const run = async (operation: () => Promise<boolean>) => {
    setBusy(true);
    setMessage("");
    const ok = await operation();
    setBusy(false);
    if (!ok) setMessage(props.dictionary.pullRequestDetail.reviewMutationFailed);
  };

  const submitReply = (pendingReviewId?: string) =>
    run(async () => {
      const result = await replyToReviewThread(
        props.owner,
        props.repository,
        props.number,
        props.thread.id,
        reply,
        props.csrfToken,
        pendingReviewId,
      );
      if (!result.ok) return false;
      props.updateThread({ ...props.thread, comments: [...props.thread.comments, result.data] });
      setReply("");
      if (pendingReviewId) props.onPendingComment();
      return true;
    });

  const toggleResolved = () =>
    run(async () => {
      const result = await setReviewThreadResolved(
        props.owner,
        props.repository,
        props.number,
        props.thread.id,
        !props.thread.resolved,
        props.thread.version,
        props.csrfToken,
      );
      if (!result.ok) return false;
      props.updateThread({ ...result.data, comments: props.thread.comments });
      return true;
    });

  const saveComment = (comment: ReviewThreadComment) =>
    run(async () => {
      const result = await updateReviewComment(
        props.owner,
        props.repository,
        props.number,
        props.thread.id,
        comment.id,
        editBody,
        comment.version,
        props.csrfToken,
      );
      if (!result.ok) return false;
      props.updateThread({
        ...props.thread,
        comments: props.thread.comments.map((item) =>
          item.id === comment.id ? { ...result.data, pending: comment.pending } : item,
        ),
      });
      setEditing(null);
      return true;
    });

  const removeComment = (comment: ReviewThreadComment) =>
    run(async () => {
      if (!window.confirm(props.dictionary.pullRequestDetail.deleteCommentConfirm)) return true;
      const result = await deleteReviewComment(
        props.owner,
        props.repository,
        props.number,
        props.thread.id,
        comment.id,
        comment.version,
        props.csrfToken,
      );
      if (!result.ok) return false;
      props.updateThread({
        ...props.thread,
        comments: props.thread.comments.map((item) =>
          item.id === comment.id ? { ...item, body: "", deleted: true, version: item.version + 1 } : item,
        ),
      });
      return true;
    });

  return (
    <article className={styles.thread} data-resolved={props.thread.resolved}>
      <header>
        <UserAvatar name={props.thread.createdBy} size={24} />
        <strong>{props.thread.createdBy}</strong>
        <span>
          {props.thread.path}:{props.thread.lineNumber}
        </span>
        <time dateTime={props.thread.createdAt} title={formatDate(props.thread.createdAt, props.locale)}>
          {formatRelativeTime(props.thread.createdAt, props.locale)}
        </time>
        {props.thread.outdated && <span>{props.dictionary.pullRequestDetail.outdatedConversation}</span>}
        {props.thread.resolved && <span>{props.dictionary.pullRequestDetail.resolved}</span>}
      </header>
      <code className={styles.anchorLine}>{props.thread.lineContent || " "}</code>
      {props.thread.comments.map((comment) => (
        <div className={styles.comment} key={comment.id}>
          <div className={styles.commentHeader}>
            <UserAvatar name={comment.author} size={22} />
            <strong>{comment.author}</strong>
            <time dateTime={comment.createdAt} title={formatDate(comment.createdAt, props.locale)}>
              {formatRelativeTime(comment.createdAt, props.locale)}
            </time>
            {comment.pending && (
              <span className={styles.pendingBadge}>{props.dictionary.pendingReviews.pendingBadge}</span>
            )}
          </div>
          {editing === comment.id ? (
            <textarea onChange={(event) => setEditBody(event.target.value)} rows={3} value={editBody} />
          ) : (
            <div className={styles.commentBody}>
              <MarkdownContent
                body={comment.deleted ? props.dictionary.pullRequestDetail.deletedComment : comment.body}
              />
            </div>
          )}
          {comment.viewerCanUpdate && !comment.deleted && (
            <div className={styles.textActions}>
              {editing === comment.id ? (
                <>
                  <button disabled={busy} onClick={() => setEditing(null)} type="button">
                    {props.dictionary.common.cancel}
                  </button>
                  <button disabled={busy || !editBody.trim()} onClick={() => void saveComment(comment)} type="button">
                    {props.dictionary.pullRequestDetail.saveComment}
                  </button>
                </>
              ) : (
                <>
                  <button
                    onClick={() => {
                      setEditing(comment.id);
                      setEditBody(comment.body);
                    }}
                    type="button"
                  >
                    {props.dictionary.pullRequestDetail.editComment}
                  </button>
                  <button disabled={busy} onClick={() => void removeComment(comment)} type="button">
                    {props.dictionary.pullRequestDetail.deleteComment}
                  </button>
                </>
              )}
            </div>
          )}
        </div>
      ))}
      {props.authenticated && (
        <div className={styles.reply}>
          <textarea
            aria-label={props.dictionary.pullRequestDetail.replyPlaceholder}
            onChange={(event) => setReply(event.target.value)}
            placeholder={props.dictionary.pullRequestDetail.replyPlaceholder}
            rows={3}
            value={reply}
          />
          <div className={styles.actions}>
            {props.thread.viewerCanResolve && (
              <button disabled={busy} onClick={() => void toggleResolved()} type="button">
                {props.thread.resolved
                  ? props.dictionary.pullRequestDetail.reopenConversation
                  : props.dictionary.pullRequestDetail.resolveConversation}
              </button>
            )}
            {props.pendingReviewId && (
              <button
                disabled={busy || !reply.trim()}
                onClick={() => void submitReply(props.pendingReviewId ?? undefined)}
                type="button"
              >
                {props.dictionary.pendingReviews.addReviewComment}
              </button>
            )}
            <button disabled={busy || !reply.trim()} onClick={() => void submitReply()} type="button">
              {props.dictionary.pullRequestDetail.reply}
            </button>
          </div>
        </div>
      )}
      <span aria-live="polite" className={styles.message}>
        {message}
      </span>
    </article>
  );
}
