"use client";

import { CheckCircle2, LockKeyhole, Pin } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type {
  AuthSession,
  Discussion,
  DiscussionCategory,
  DiscussionComment,
  DiscussionSummary,
} from "@/lib/api-types";
import { deleteJson, patchJson, postJson, putJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { loginUrl, repositoryPath } from "@/lib/routes";

import { MarkdownContent } from "../wiki/markdown-content";
import { DiscussionCommentComposer } from "./discussion-comment-composer";
import { DiscussionCommentPagination } from "./discussion-comment-pagination";
import { DiscussionCommentTree } from "./discussion-comment-tree";
import styles from "./discussion-detail.module.css";
import { DiscussionEditor } from "./discussion-editor";
import { DiscussionHeading } from "./discussion-heading";

type DiscussionDetailProps = {
  categories: DiscussionCategory[];
  dictionary: Dictionary;
  discussion: Discussion;
  locale: Locale;
  owner: string;
  repository: string;
  session: AuthSession;
};

// The detail view coordinates thread mutations and passes them to focused child components.
export function DiscussionDetail(props: DiscussionDetailProps) {
  const router = useRouter();
  const copy = props.dictionary.discussionsPage;
  const [discussion, setDiscussion] = useState(props.discussion);
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [message, setMessage] = useState("");
  const csrfToken = props.session.status === "authenticated" ? props.session.csrfToken : "";
  const apiPath = `/api/v1/repositories/${encodeURIComponent(props.owner)}/${encodeURIComponent(
    props.repository,
  )}/discussions/${discussion.number}`;
  const canChangeState = discussion.viewerCanEdit || discussion.viewerCanModerate;

  async function mutateDiscussion(input: Record<string, unknown>) {
    if (!csrfToken) return false;
    setBusy("discussion");
    setMessage("");
    const result = await patchJson<Discussion>(apiPath, input, csrfToken);
    setBusy(null);
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, props.dictionary));
      return false;
    }
    setDiscussion((current) => preserveCommentPage(current, result.data));
    setEditing(false);
    router.refresh();
    return true;
  }

  async function toggleVote() {
    if (!csrfToken) return;
    setBusy("vote");
    setMessage("");
    const path = `${apiPath}/vote`;
    const result = discussion.viewerHasVoted
      ? await deleteJson<DiscussionSummary>(path, csrfToken)
      : await putJson<DiscussionSummary>(path, undefined, csrfToken);
    setBusy(null);
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, props.dictionary));
      return;
    }
    setDiscussion((current) => ({ ...current, ...result.data }));
  }

  async function createComment(body: string, parentId: string | null): Promise<boolean> {
    if (!csrfToken) return false;
    setBusy(parentId ?? "comment");
    setMessage("");
    const result = await postJson<DiscussionComment>(
      `${apiPath}/comments`,
      { body, parentId: parentId || undefined },
      csrfToken,
    );
    setBusy(null);
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, props.dictionary));
      return false;
    }
    router.refresh();
    return true;
  }

  async function updateComment(commentID: string, body: string): Promise<boolean> {
    if (!csrfToken) return false;
    setBusy(commentID);
    setMessage("");
    const result = await patchJson<DiscussionComment>(
      `${apiPath}/comments/${encodeURIComponent(commentID)}`,
      { body },
      csrfToken,
    );
    setBusy(null);
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, props.dictionary));
      return false;
    }
    router.refresh();
    return true;
  }

  async function deleteComment(commentID: string): Promise<boolean> {
    if (!csrfToken) return false;
    if (!window.confirm(copy.deleteConfirm)) return false;
    setBusy(commentID);
    setMessage("");
    const result = await deleteJson<null>(`${apiPath}/comments/${encodeURIComponent(commentID)}`, csrfToken);
    setBusy(null);
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, props.dictionary));
      return false;
    }
    router.refresh();
    return true;
  }

  async function deleteDiscussion() {
    if (!csrfToken || !window.confirm(copy.deleteDiscussionConfirm)) return;
    setBusy("delete-discussion");
    setMessage("");
    const result = await deleteJson<null>(apiPath, csrfToken);
    setBusy(null);
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, props.dictionary));
      return;
    }
    router.push(repositoryPath(props.locale, props.owner, props.repository, "discussions"));
    router.refresh();
  }

  async function setAnswer(commentID: string, accepted: boolean) {
    if (!csrfToken) return;
    setBusy(commentID);
    setMessage("");
    const path = `${apiPath}/comments/${encodeURIComponent(commentID)}/answer`;
    const result = accepted
      ? await putJson<Discussion>(path, undefined, csrfToken)
      : await deleteJson<Discussion>(path, csrfToken);
    setBusy(null);
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, props.dictionary));
      return;
    }
    setDiscussion((current) => ({
      ...preserveCommentPage(current, result.data),
      comments: current.comments.map((comment) => ({
        ...comment,
        answer: accepted ? comment.id === commentID : comment.id === commentID ? false : comment.answer,
      })),
    }));
    router.refresh();
  }

  return (
    <div className={styles.page}>
      <DiscussionHeading
        busy={busy}
        canChangeState={canChangeState}
        discussion={discussion}
        onDelete={deleteDiscussion}
        onEdit={() => setEditing((current) => !current)}
        onToggleState={() => mutateDiscussion({ state: discussion.state === "open" ? "closed" : "open" })}
        onToggleLock={() => mutateDiscussion({ locked: !discussion.locked })}
        onTogglePin={() => mutateDiscussion({ pinned: !discussion.pinned })}
        onToggleVote={toggleVote}
        session={props.session}
        copy={copy}
        locale={props.locale}
      />
      {message && (
        <p className={styles.notice} role="alert">
          {message}
        </p>
      )}
      {editing ? (
        <DiscussionEditor
          body={discussion.body}
          busy={busy === "discussion"}
          categories={props.categories}
          category={discussion.category.slug}
          copy={copy}
          onCancel={() => setEditing(false)}
          onSave={mutateDiscussion}
          title={discussion.title}
          viewerCanModerate={discussion.viewerCanModerate}
        />
      ) : (
        <article className={styles.card}>
          <header className={styles.cardHeader}>
            <strong>{discussion.author.displayName}</strong>
            <span>@{discussion.author.username}</span>
            <time dateTime={discussion.createdAt}>{formatDate(discussion.createdAt, props.locale)}</time>
          </header>
          <div className={`${styles.cardBody} ${styles.markdown}`}>
            <MarkdownContent body={discussion.body} />
          </div>
        </article>
      )}
      <div className={styles.badges}>
        {discussion.locked && (
          <span className={styles.badge}>
            <LockKeyhole size={13} /> {copy.locked}
          </span>
        )}
        {discussion.pinned && (
          <span className={styles.badge}>
            <Pin size={13} /> {copy.pinned}
          </span>
        )}
        {discussion.answered && (
          <span className={styles.badge}>
            <CheckCircle2 size={13} /> {copy.answered}
          </span>
        )}
      </div>
      {discussion.comments.length > 0 && (
        <section className={styles.comments}>
          {discussion.comments
            .filter(
              (comment) => !comment.parentId || !discussion.comments.some((parent) => parent.id === comment.parentId),
            )
            .map((comment) => (
              <DiscussionCommentTree
                busy={busy}
                canComment={discussion.viewerCanComment}
                comment={comment}
                comments={discussion.comments}
                copy={copy}
                key={comment.id}
                locale={props.locale}
                onAnswer={setAnswer}
                onCreate={createComment}
                onDelete={deleteComment}
                onUpdate={updateComment}
              />
            ))}
        </section>
      )}
      <DiscussionCommentPagination
        commentPage={discussion.commentPage}
        commentsPerPage={discussion.commentsPerPage}
        copy={copy}
        discussionNumber={discussion.number}
        locale={props.locale}
        owner={props.owner}
        repository={props.repository}
        totalComments={discussion.totalComments}
      />
      {discussion.viewerCanComment ? (
        <DiscussionCommentComposer
          busy={busy === "comment"}
          copy={copy}
          onSubmit={(body) => createComment(body, null)}
        />
      ) : props.session.status !== "authenticated" ? (
        <p className={styles.login}>
          <Link href={loginUrl(repositoryPath(props.locale, props.owner, props.repository, "discussions"))}>
            {copy.signInToComment}
          </Link>
        </p>
      ) : null}
    </div>
  );
}

function formatDate(value: string, locale: Locale): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short", timeZone: "UTC" }).format(
    new Date(value),
  );
}

function preserveCommentPage(current: Discussion, next: Discussion): Discussion {
  return {
    ...current,
    ...next,
    comments: current.comments,
    commentPage: current.commentPage,
    commentsPerPage: current.commentsPerPage,
    totalComments: current.totalComments,
  };
}
