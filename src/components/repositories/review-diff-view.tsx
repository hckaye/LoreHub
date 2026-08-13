"use client";

import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { LoreDiff, ReviewThread, ReviewThreadComment } from "@/lib/api-types";
import { abbreviateCount, formatDate, formatRelativeTime } from "@/lib/format";
import { parseReviewDiff, type ReviewDiffRow } from "@/lib/review-diff";
import {
  createReviewThread,
  deleteReviewComment,
  replyToReviewThread,
  setReviewThreadResolved,
  updateReviewComment,
} from "@/lib/review-thread-client";

import { UserAvatar } from "../ui/user-avatar";
import { MarkdownContent } from "../wiki/markdown-content";
import styles from "./review-diff-view.module.css";

type Anchor = { path: string; side: "left" | "right"; lineNumber: number };

type ReviewDiffViewProps = {
  diff: LoreDiff;
  threads: ReviewThread[];
  available: boolean;
  owner: string;
  repository: string;
  number: number;
  locale: Locale;
  csrfToken: string;
  authenticated: boolean;
  dictionary: Dictionary;
};

export function ReviewDiffView(props: ReviewDiffViewProps) {
  const [threads, setThreads] = useState(props.threads);
  const [anchor, setAnchor] = useState<Anchor | null>(null);
  const [body, setBody] = useState("");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState(
    props.available ? "" : props.dictionary.pullRequestDetail.reviewThreadsUnavailable,
  );

  const submitThread = async () => {
    if (!anchor || !body.trim() || !props.csrfToken) return;
    setBusy(true);
    setMessage("");
    const result = await createReviewThread(
      props.owner,
      props.repository,
      props.number,
      {
        ...anchor,
        body,
        expectedBaseRevision: props.diff.source,
        expectedHeadRevision: props.diff.target,
      },
      props.csrfToken,
    );
    setBusy(false);
    if (!result.ok) {
      setMessage(mutationMessage(result.kind, props.dictionary));
      return;
    }
    setThreads((current) => [...current, result.data]);
    setAnchor(null);
    setBody("");
  };

  const updateThread = (thread: ReviewThread) => {
    setThreads((current) => current.map((item) => (item.id === thread.id ? thread : item)));
  };

  const outdated = threads.filter((thread) => thread.outdated);
  const stats = diffStats(props.diff);
  if (props.diff.files.length === 0) {
    return <p className={styles.meta}>{props.dictionary.pullRequestDetail.noChangedFiles}</p>;
  }
  return (
    <div className={styles.files}>
      <p className={styles.summary}>
        {props.dictionary.pullRequestDetail.filesChangedSummary
          .replace("{files}", abbreviateCount(props.diff.files.length, props.locale))
          .replace("{additions}", abbreviateCount(stats.additions, props.locale))
          .replace("{deletions}", abbreviateCount(stats.deletions, props.locale))}
      </p>
      <div aria-live="polite" className={styles.message}>
        {message}
      </div>
      {props.diff.files.map((file) => {
        const currentThreads = threads.filter((thread) => thread.path === file.path && !thread.outdated);
        return (
          <details className={styles.file} key={file.path} open>
            <summary className={styles.fileHeader}>
              <h3>{file.path}</h3>
              <span>{actionLabel(file.action, props.dictionary)}</span>
            </summary>
            {file.binary || !file.patch ? (
              <p className={styles.meta}>
                {file.binary ? props.dictionary.codeBrowser.binary : props.dictionary.codeBrowser.diffTruncated}
              </p>
            ) : (
              <table className={styles.diffTable}>
                <tbody>
                  {parseReviewDiff(file).map((row) => (
                    <DiffRow
                      {...props}
                      anchor={anchor}
                      authenticated={props.authenticated}
                      busy={busy}
                      dictionary={props.dictionary}
                      key={row.key}
                      onCancel={() => setAnchor(null)}
                      onChangeBody={setBody}
                      onSelect={setAnchor}
                      onSubmit={submitThread}
                      path={file.path}
                      row={row}
                      threadBody={body}
                      threads={threadsForRow(currentThreads, row)}
                      updateThread={updateThread}
                    />
                  ))}
                </tbody>
              </table>
            )}
            {file.truncated && <p className={styles.meta}>{props.dictionary.codeBrowser.diffTruncated}</p>}
          </details>
        );
      })}
      {outdated.length > 0 && (
        <section className={styles.outdated}>
          <h3>{props.dictionary.pullRequestDetail.outdatedConversations}</h3>
          {outdated.map((thread) => (
            <ReviewThreadCard key={thread.id} thread={thread} updateThread={updateThread} {...props} />
          ))}
        </section>
      )}
      {(props.diff.hasMore || props.diff.truncated) && (
        <p className={styles.meta}>{props.dictionary.codeBrowser.diffTruncated}</p>
      )}
    </div>
  );
}

type DiffRowProps = ReviewDiffViewProps & {
  path: string;
  row: ReviewDiffRow;
  threads: ReviewThread[];
  anchor: Anchor | null;
  threadBody: string;
  busy: boolean;
  onSelect: (anchor: Anchor) => void;
  onCancel: () => void;
  onChangeBody: (body: string) => void;
  onSubmit: () => Promise<void>;
  updateThread: (thread: ReviewThread) => void;
};

function DiffRow(props: DiffRowProps) {
  const { row } = props;
  if (row.kind === "header") {
    return (
      <tr className={styles.hunk}>
        <td colSpan={3}>{row.content}</td>
      </tr>
    );
  }
  const selected =
    props.anchor?.path === props.path &&
    ((props.anchor.side === "left" && props.anchor.lineNumber === row.oldLine) ||
      (props.anchor.side === "right" && props.anchor.lineNumber === row.newLine));
  return (
    <>
      <tr className={styles.line} data-kind={row.kind}>
        <LineNumber
          authenticated={props.authenticated}
          dictionary={props.dictionary}
          line={row.oldLine}
          onSelect={(lineNumber) => props.onSelect({ path: props.path, side: "left", lineNumber })}
          side="left"
        />
        <LineNumber
          authenticated={props.authenticated}
          dictionary={props.dictionary}
          line={row.newLine}
          onSelect={(lineNumber) => props.onSelect({ path: props.path, side: "right", lineNumber })}
          side="right"
        />
        <td className={styles.code}>
          <code>{row.content || " "}</code>
        </td>
      </tr>
      {selected && (
        <tr>
          <td className={styles.inline} colSpan={3}>
            <ReviewComposer
              body={props.threadBody}
              busy={props.busy}
              dictionary={props.dictionary}
              onCancel={props.onCancel}
              onChange={props.onChangeBody}
              onSubmit={props.onSubmit}
            />
          </td>
        </tr>
      )}
      {props.threads.map((thread) => (
        <tr key={thread.id}>
          <td className={styles.inline} colSpan={3}>
            <ReviewThreadCard {...props} thread={thread} updateThread={props.updateThread} />
          </td>
        </tr>
      ))}
    </>
  );
}

function LineNumber({
  line,
  side,
  authenticated,
  dictionary,
  onSelect,
}: {
  line: number | null;
  side: "left" | "right";
  authenticated: boolean;
  dictionary: Dictionary;
  onSelect: (line: number) => void;
}) {
  if (line === null) return <td className={styles.lineNumber} />;
  const label = dictionary.pullRequestDetail.commentOnLine.replace("{line}", String(line));
  return (
    <td className={styles.lineNumber}>
      {authenticated ? (
        <button aria-label={`${label} (${side})`} onClick={() => onSelect(line)} type="button">
          {line}
        </button>
      ) : (
        line
      )}
    </td>
  );
}

function ReviewComposer({
  body,
  busy,
  dictionary,
  onChange,
  onCancel,
  onSubmit,
}: {
  body: string;
  busy: boolean;
  dictionary: Dictionary;
  onChange: (value: string) => void;
  onCancel: () => void;
  onSubmit: () => Promise<void>;
}) {
  return (
    <div className={styles.composer}>
      <textarea
        aria-label={dictionary.pullRequestDetail.newThreadPlaceholder}
        onChange={(event) => onChange(event.target.value)}
        placeholder={dictionary.pullRequestDetail.newThreadPlaceholder}
        rows={4}
        value={body}
      />
      <div className={styles.actions}>
        <button disabled={busy} onClick={onCancel} type="button">
          {dictionary.common.cancel}
        </button>
        <button disabled={busy || !body.trim()} onClick={() => void onSubmit()} type="button">
          {dictionary.pullRequestDetail.startThread}
        </button>
      </div>
    </div>
  );
}

type ThreadCardProps = ReviewDiffViewProps & {
  thread: ReviewThread;
  updateThread: (thread: ReviewThread) => void;
};

function ReviewThreadCard(props: ThreadCardProps) {
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

  const submitReply = () =>
    run(async () => {
      const result = await replyToReviewThread(
        props.owner,
        props.repository,
        props.number,
        props.thread.id,
        reply,
        props.csrfToken,
      );
      if (!result.ok) return false;
      props.updateThread({ ...props.thread, comments: [...props.thread.comments, result.data] });
      setReply("");
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
        comments: props.thread.comments.map((item) => (item.id === comment.id ? result.data : item)),
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

function threadsForRow(threads: ReviewThread[], row: ReviewDiffRow): ReviewThread[] {
  return threads.filter(
    (thread) =>
      (thread.side === "left" && thread.lineNumber === row.oldLine) ||
      (thread.side === "right" && thread.lineNumber === row.newLine),
  );
}

function mutationMessage(kind: string, dictionary: Dictionary): string {
  return kind === "conflict"
    ? dictionary.pullRequestDetail.reviewConflict
    : dictionary.pullRequestDetail.reviewMutationFailed;
}

function actionLabel(action: string, dictionary: Dictionary): string {
  switch (action) {
    case "added":
      return dictionary.codeBrowser.actions.added;
    case "deleted":
      return dictionary.codeBrowser.actions.deleted;
    case "moved":
      return dictionary.codeBrowser.actions.moved;
    case "copied":
      return dictionary.codeBrowser.actions.copied;
    default:
      return dictionary.codeBrowser.actions.modified;
  }
}

function diffStats(diff: LoreDiff): { additions: number; deletions: number } {
  return diff.files.reduce(
    (stats, file) => {
      for (const line of file.patch?.split("\n") ?? []) {
        if (line.startsWith("+++") || line.startsWith("---")) continue;
        if (line.startsWith("+")) stats.additions += 1;
        if (line.startsWith("-")) stats.deletions += 1;
      }
      return stats;
    },
    { additions: 0, deletions: 0 },
  );
}
