"use client";

import { CheckCircle2 } from "lucide-react";
import { FormEvent, useState } from "react";

import { formatTimestamp } from "@/lib/format";

import { MarkdownContent } from "../wiki/markdown-content";
import type { DiscussionCommentCardProps } from "./discussion-component-types";
import styles from "./discussion-detail.module.css";

export function DiscussionCommentCard(props: DiscussionCommentCardProps) {
  const [editing, setEditing] = useState(false);
  const [replying, setReplying] = useState(false);
  const [body, setBody] = useState(props.comment.body);
  const [reply, setReply] = useState("");

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (await props.onUpdate(props.comment.id, body)) setEditing(false);
  }

  async function submitReply(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (await props.onCreate(reply, props.comment.id)) {
      setReply("");
      setReplying(false);
    }
  }

  const canReply = props.canComment && props.comment.parentId === null;
  return (
    <article
      className={`${styles.card} ${props.comment.answer ? styles.answer : ""} ${styles.comment}`}
      data-root={props.root}
    >
      <header className={styles.cardHeader}>
        <strong>{props.comment.author.displayName}</strong>
        <span>@{props.comment.author.username}</span>
        <time dateTime={props.comment.createdAt}>{formatTimestamp(props.comment.createdAt, props.locale)}</time>
        {props.comment.editedAt && <span>{props.copy.edited}</span>}
        {props.comment.answer && (
          <span className={styles.answerLabel}>
            <CheckCircle2 size={13} /> {props.copy.answered}
          </span>
        )}
      </header>
      <div className={styles.cardBody}>
        {editing ? (
          <form className={styles.commentEditor} onSubmit={save}>
            <textarea maxLength={262_144} onChange={(event) => setBody(event.target.value)} required value={body} />
            <div className={styles.actions}>
              <button className={styles.button} disabled={props.busy === props.comment.id} type="submit">
                {props.copy.save}
              </button>
              <button className={styles.secondaryButton} onClick={() => setEditing(false)} type="button">
                {props.copy.cancel}
              </button>
            </div>
          </form>
        ) : (
          <div className={styles.markdown}>
            <MarkdownContent body={props.comment.body} />
          </div>
        )}
        <div className={styles.commentActions}>
          {props.comment.viewerCanEdit && !editing && (
            <button className={styles.textButton} onClick={() => setEditing(true)} type="button">
              {props.copy.edit}
            </button>
          )}
          {props.comment.viewerCanDelete && (
            <button
              className={styles.textButton}
              disabled={props.busy === props.comment.id}
              onClick={() => void props.onDelete(props.comment.id)}
              type="button"
            >
              {props.copy.delete}
            </button>
          )}
          {canReply && (
            <button className={styles.textButton} onClick={() => setReplying((current) => !current)} type="button">
              {props.copy.reply}
            </button>
          )}
          {props.comment.viewerCanMarkAnswer && (
            <button
              className={styles.textButton}
              disabled={props.busy === props.comment.id}
              onClick={() => void props.onAnswer(props.comment.id, !props.comment.answer)}
              type="button"
            >
              {props.comment.answer ? props.copy.unmarkAnswer : props.copy.markAnswer}
            </button>
          )}
        </div>
        {replying && canReply && (
          <form className={`${styles.commentEditor} ${styles.replyForm}`} onSubmit={submitReply}>
            <label htmlFor={`reply-${props.comment.id}`}>
              {props.copy.replyTo.replace("{name}", props.comment.author.displayName)}
            </label>
            <textarea
              id={`reply-${props.comment.id}`}
              maxLength={262_144}
              onChange={(event) => setReply(event.target.value)}
              required
              value={reply}
            />
            <button className={styles.button} disabled={props.busy === props.comment.id} type="submit">
              {props.copy.submitReply}
            </button>
          </form>
        )}
      </div>
    </article>
  );
}
