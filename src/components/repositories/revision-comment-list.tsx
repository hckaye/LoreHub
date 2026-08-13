"use client";

import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import { formatTimestamp } from "@/lib/format";
import { revisionCommentPageHref, type RevisionComment, type RevisionCommentPage } from "@/lib/revision-comments";

import { MarkdownContent } from "../wiki/markdown-content";
import { RevisionCommentPagination } from "./revision-comment-pagination";
import styles from "./revision-comments.module.css";

type RevisionCommentListProps = {
  basePath: string;
  busy: string | null;
  comments: RevisionCommentPage;
  dictionary: Dictionary;
  locale: Locale;
  onDelete: (commentID: string) => Promise<boolean>;
  onUpdate: (commentID: string, body: string) => Promise<boolean>;
  revision: string;
};

export function RevisionCommentList(props: RevisionCommentListProps) {
  if (props.comments.items.length === 0 && props.comments.page === 1) {
    return <p className={styles.empty}>{props.dictionary.revisionComments.empty}</p>;
  }
  return (
    <div className={styles.list}>
      {props.comments.items.map((comment) => (
        <RevisionCommentCard
          busy={props.busy === comment.id}
          comment={comment}
          dictionary={props.dictionary}
          key={comment.id}
          locale={props.locale}
          onDelete={props.onDelete}
          onUpdate={props.onUpdate}
        />
      ))}
      <RevisionCommentPagination
        dictionary={props.dictionary}
        hasNext={props.comments.hasNext}
        href={(page) => revisionCommentPageHref(props.basePath, props.revision, page)}
        page={props.comments.page}
        perPage={props.comments.perPage}
        totalCount={props.comments.totalCount}
      />
    </div>
  );
}

function RevisionCommentCard(props: {
  busy: boolean;
  comment: RevisionComment;
  dictionary: Dictionary;
  locale: Locale;
  onDelete: (commentID: string) => Promise<boolean>;
  onUpdate: (commentID: string, body: string) => Promise<boolean>;
}) {
  const [editing, setEditing] = useState(false);
  const [body, setBody] = useState(props.comment.body);
  const copy = props.dictionary.revisionComments;
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (await props.onUpdate(props.comment.id, body)) setEditing(false);
  }
  async function remove() {
    if (window.confirm(copy.deleteConfirm)) await props.onDelete(props.comment.id);
  }
  return (
    <article className={styles.card}>
      <header>
        <strong>{authorName(props.comment)}</strong>
        <time dateTime={props.comment.createdAt}>{formatTimestamp(props.comment.createdAt, props.locale)}</time>
        {props.comment.editedAt && <span>{copy.edited}</span>}
        {props.comment.viewerCanUpdate && !editing && (
          <span className={styles.cardActions}>
            <button onClick={() => setEditing(true)} type="button">
              {copy.edit}
            </button>
            <button disabled={props.busy} onClick={remove} type="button">
              {copy.delete}
            </button>
          </span>
        )}
      </header>
      {editing ? (
        <form className={styles.editForm} onSubmit={submit}>
          <textarea maxLength={1_000_000} onChange={(event) => setBody(event.target.value)} required value={body} />
          <div className={styles.formActions}>
            <button disabled={props.busy || body.trim() === ""} type="submit">
              {copy.save}
            </button>
            <button className={styles.secondary} onClick={() => setEditing(false)} type="button">
              {props.dictionary.common.cancel}
            </button>
          </div>
        </form>
      ) : (
        <div className={styles.body}>
          <MarkdownContent body={props.comment.body} />
        </div>
      )}
    </article>
  );
}

function authorName(comment: RevisionComment): string {
  return comment.author.displayName
    ? `${comment.author.displayName} (@${comment.author.username})`
    : `@${comment.author.username}`;
}
