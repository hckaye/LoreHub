"use client";

import { FormEvent, useEffect, useRef, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Issue, IssueComment } from "@/lib/api-types";
import type { CommentPage } from "@/lib/comment-page-types";

import { AuthRequired } from "../auth/auth-required";
import { MarkdownContent } from "../wiki/markdown-content";
import { ConversationPagination } from "./conversation-pagination";
import { CommentComposer } from "./issue-comment-composer";
import styles from "./issue-detail.module.css";
import { MarkdownField } from "./issue-markdown-field";
import { TimelineItem } from "./issue-timeline-item";

type ConversationProps = {
  busyAction: string | null;
  basePath: string;
  comments: CommentPage<IssueComment> | null;
  dictionary: Dictionary;
  issue: Issue;
  locale: Locale;
  onDeleteComment: (commentID: string) => Promise<boolean>;
  onSubmitComment: (body: string, nextState: "open" | "closed" | null) => Promise<boolean>;
  onUpdateComment: (commentID: string, body: string) => Promise<boolean>;
  onUpdateIssue: (input: Partial<Pick<Issue, "title" | "body" | "state">>) => Promise<boolean>;
  session: AuthSession;
};

export function IssueConversation(props: ConversationProps) {
  const [editingIssue, setEditingIssue] = useState(false);
  return (
    <div className={styles.conversation}>
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
      {!props.comments && <p className={styles.notice}>{props.dictionary.issueDetail.commentsUnavailable}</p>}
      {(props.comments?.items ?? []).map((comment) => (
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
      {props.comments && (
        <ConversationPagination
          basePath={props.basePath}
          dictionary={props.dictionary}
          hasNext={props.comments.hasNext}
          page={props.comments.page}
          perPage={props.comments.perPage}
          totalCount={props.comments.totalCount}
        />
      )}
      {props.session.status === "authenticated" ? (
        <CommentComposer
          busy={props.busyAction === "new-comment"}
          dictionary={props.dictionary}
          issue={props.issue}
          onSubmit={props.onSubmitComment}
          viewer={props.session.user}
        />
      ) : (
        <AuthRequired dictionary={props.dictionary} returnTo={props.basePath} session={props.session} />
      )}
    </div>
  );
}

function IssueBody(props: { dictionary: Dictionary; issue: Issue; locale: Locale; onEdit: () => void }) {
  const copy = props.dictionary.issueDetail;
  return (
    <TimelineItem
      actions={
        props.issue.viewerCanUpdate && (
          <button className={styles.textButton} onClick={props.onEdit} type="button">
            {copy.editIssue}
          </button>
        )
      }
      author={props.issue.author}
      locale={props.locale}
      template={copy.commented}
      timestamp={props.issue.createdAt}
    >
      <div className={styles.body}>
        {props.issue.body ? (
          <MarkdownContent body={props.issue.body} />
        ) : (
          <p className={styles.empty}>{copy.noDescription}</p>
        )}
      </div>
    </TimelineItem>
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
      <label className={styles.fieldLabel} htmlFor="issue-detail-title">
        {props.dictionary.forms.titleLabel}
      </label>
      <input
        id="issue-detail-title"
        maxLength={512}
        onChange={(event) => setTitle(event.target.value)}
        required
        value={title}
      />
      <MarkdownField
        dictionary={props.dictionary}
        id="issue-detail-body"
        label={props.dictionary.forms.bodyLabel}
        onChange={setBody}
        value={body}
      />
      <div className={styles.actions}>
        <button className={styles.primaryButton} disabled={props.busyAction === "issue"} type="submit">
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
  const copy = props.dictionary.issueDetail;
  const [editing, setEditing] = useState(false);
  const [body, setBody] = useState(props.comment.body);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (await props.onUpdate(props.comment.id, body)) setEditing(false);
  }
  return (
    <TimelineItem
      actions={
        props.comment.viewerCanUpdate &&
        !editing && (
          <CommentActions
            busy={props.busy}
            dictionary={props.dictionary}
            onDelete={() => props.onDelete(props.comment.id)}
            onEdit={() => setEditing(true)}
          />
        )
      }
      author={props.comment.author}
      locale={props.locale}
      meta={props.comment.editedAt && <span className={styles.edited}>{copy.edited}</span>}
      template={copy.commented}
      timestamp={props.comment.createdAt}
    >
      {editing ? (
        <form className={styles.commentEditor} onSubmit={submit}>
          <MarkdownField
            dictionary={props.dictionary}
            id={`issue-comment-${props.comment.id}`}
            label={copy.editComment}
            labelHidden
            onChange={setBody}
            value={body}
          />
          <div className={styles.actions}>
            <button className={styles.primaryButton} disabled={props.busy} type="submit">
              {copy.saveChanges}
            </button>
            <button className={styles.secondaryButton} onClick={() => setEditing(false)} type="button">
              {props.dictionary.common.cancel}
            </button>
          </div>
        </form>
      ) : (
        <div className={styles.body}>
          <MarkdownContent body={props.comment.body} />
        </div>
      )}
    </TimelineItem>
  );
}

function CommentActions(props: {
  busy: boolean;
  dictionary: Dictionary;
  onDelete: () => Promise<boolean>;
  onEdit: () => void;
}) {
  const copy = props.dictionary.issueDetail;
  const [confirming, setConfirming] = useState(false);
  const confirmRef = useRef<HTMLButtonElement>(null);
  const deleteRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (confirming) confirmRef.current?.focus();
  }, [confirming]);
  function cancel() {
    setConfirming(false);
    deleteRef.current?.focus();
  }
  if (confirming) {
    return (
      <span
        className={styles.confirm}
        onKeyDown={(event) => {
          if (event.key === "Escape") cancel();
        }}
        role="group"
        aria-label={copy.deleteCommentConfirm}
      >
        <span>{copy.deleteCommentConfirm}</span>
        <button
          className={styles.dangerButton}
          disabled={props.busy}
          onClick={() => props.onDelete()}
          ref={confirmRef}
          type="button"
        >
          {copy.confirmDeleteComment}
        </button>
        <button className={styles.textButton} onClick={cancel} type="button">
          {props.dictionary.common.cancel}
        </button>
      </span>
    );
  }
  return (
    <>
      <button className={styles.textButton} onClick={props.onEdit} type="button">
        {copy.editComment}
      </button>
      <button
        className={styles.textButton}
        disabled={props.busy}
        onClick={() => setConfirming(true)}
        ref={deleteRef}
        type="button"
      >
        {copy.deleteComment}
      </button>
    </>
  );
}
