"use client";

import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Issue, IssueComment } from "@/lib/api-types";

import styles from "./issue-detail.module.css";

type ConversationProps = {
  busyAction: string | null;
  comments: IssueComment[];
  commentsAvailable: boolean;
  dictionary: Dictionary;
  issue: Issue;
  locale: Locale;
  onCreateComment: (body: string) => Promise<boolean>;
  onDeleteComment: (commentID: string) => Promise<boolean>;
  onUpdateComment: (commentID: string, body: string) => Promise<boolean>;
  onUpdateIssue: (input: Partial<Pick<Issue, "title" | "body" | "state">>) => Promise<boolean>;
  session: AuthSession;
};

export function IssueConversation(props: ConversationProps) {
  const [editingIssue, setEditingIssue] = useState(false);
  return (
    <main className={styles.conversation}>
      {editingIssue ? (
        <IssueEditForm {...props} onCancel={() => setEditingIssue(false)} />
      ) : (
        <IssueBody
          dictionary={props.dictionary}
          issue={props.issue}
          locale={props.locale}
          onEdit={() => setEditingIssue(true)}
        />
      )}
      {!props.commentsAvailable && <p className={styles.notice}>{props.dictionary.issueDetail.commentsUnavailable}</p>}
      {props.comments.map((comment) => (
        <CommentCard
          busy={props.busyAction === comment.id}
          comment={comment}
          dictionary={props.dictionary}
          key={comment.id}
          locale={props.locale}
          onDelete={props.onDeleteComment}
          onUpdate={props.onUpdateComment}
        />
      ))}
      {props.session.status === "authenticated" && (
        <NewCommentForm
          busy={props.busyAction === "new-comment"}
          dictionary={props.dictionary}
          onSubmit={props.onCreateComment}
        />
      )}
    </main>
  );
}

function IssueBody(props: { dictionary: Dictionary; issue: Issue; locale: Locale; onEdit: () => void }) {
  return (
    <article className={styles.card}>
      <header>
        <strong>{props.issue.author}</strong>
        <time dateTime={props.issue.createdAt}>{formatDate(props.issue.createdAt, props.locale)}</time>
        {props.issue.viewerCanUpdate && (
          <button className={styles.textButton} onClick={props.onEdit} type="button">
            {props.dictionary.issueDetail.editIssue}
          </button>
        )}
      </header>
      <div className={styles.body}>{props.issue.body || props.dictionary.issueDetail.noDescription}</div>
    </article>
  );
}

function IssueEditForm(props: ConversationProps & { onCancel: () => void }) {
  const [title, setTitle] = useState(props.issue.title);
  const [body, setBody] = useState(props.issue.body);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (await props.onUpdateIssue({ title: title.trim(), body })) props.onCancel();
  }
  return (
    <form className={styles.editor} onSubmit={submit}>
      <label htmlFor="issue-detail-title">{props.dictionary.forms.titleLabel}</label>
      <input
        id="issue-detail-title"
        maxLength={512}
        onChange={(event) => setTitle(event.target.value)}
        required
        value={title}
      />
      <label htmlFor="issue-detail-body">{props.dictionary.forms.bodyLabel}</label>
      <textarea
        id="issue-detail-body"
        maxLength={1_000_000}
        onChange={(event) => setBody(event.target.value)}
        value={body}
      />
      <div className={styles.actions}>
        <button disabled={props.busyAction === "issue"} type="submit">
          {props.dictionary.issueDetail.saveChanges}
        </button>
        <button className={styles.secondaryButton} onClick={props.onCancel} type="button">
          {props.dictionary.common.cancel}
        </button>
      </div>
    </form>
  );
}

function CommentCard(props: {
  busy: boolean;
  comment: IssueComment;
  dictionary: Dictionary;
  locale: Locale;
  onDelete: (commentID: string) => Promise<boolean>;
  onUpdate: (commentID: string, body: string) => Promise<boolean>;
}) {
  const [editing, setEditing] = useState(false);
  const [body, setBody] = useState(props.comment.body);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (await props.onUpdate(props.comment.id, body)) setEditing(false);
  }
  async function remove() {
    if (!window.confirm(props.dictionary.issueDetail.deleteCommentConfirm)) return;
    await props.onDelete(props.comment.id);
  }
  return (
    <article className={styles.card}>
      <header>
        <strong>{props.comment.author}</strong>
        <time dateTime={props.comment.createdAt}>{formatDate(props.comment.createdAt, props.locale)}</time>
        {props.comment.editedAt && <span>{props.dictionary.issueDetail.edited}</span>}
        {props.comment.viewerCanUpdate && !editing && (
          <span className={styles.cardActions}>
            <button className={styles.textButton} onClick={() => setEditing(true)} type="button">
              {props.dictionary.issueDetail.editComment}
            </button>
            <button className={styles.textButton} disabled={props.busy} onClick={remove} type="button">
              {props.dictionary.issueDetail.deleteComment}
            </button>
          </span>
        )}
      </header>
      {editing ? (
        <form className={styles.commentEditor} onSubmit={submit}>
          <textarea required value={body} onChange={(event) => setBody(event.target.value)} />
          <div className={styles.actions}>
            <button disabled={props.busy} type="submit">
              {props.dictionary.issueDetail.saveChanges}
            </button>
            <button className={styles.secondaryButton} onClick={() => setEditing(false)} type="button">
              {props.dictionary.common.cancel}
            </button>
          </div>
        </form>
      ) : (
        <div className={styles.body}>{props.comment.body}</div>
      )}
    </article>
  );
}

function NewCommentForm(props: {
  busy: boolean;
  dictionary: Dictionary;
  onSubmit: (body: string) => Promise<boolean>;
}) {
  const [body, setBody] = useState("");
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (await props.onSubmit(body)) setBody("");
  }
  return (
    <form className={styles.editor} onSubmit={submit}>
      <label htmlFor="new-issue-comment">{props.dictionary.issueDetail.addComment}</label>
      <textarea
        id="new-issue-comment"
        maxLength={1_000_000}
        onChange={(event) => setBody(event.target.value)}
        placeholder={props.dictionary.issueDetail.commentPlaceholder}
        required
        value={body}
      />
      <div className={styles.actions}>
        <button disabled={props.busy} type="submit">
          {props.dictionary.issueDetail.submitComment}
        </button>
      </div>
    </form>
  );
}

function formatDate(value: string, locale: Locale): string {
  return new Intl.DateTimeFormat(locale, {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: "UTC",
  }).format(new Date(value));
}
