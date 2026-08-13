"use client";

import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthUser, Issue } from "@/lib/api-types";

import { UserAvatar } from "../ui/user-avatar";
import styles from "./issue-detail.module.css";
import { MarkdownField } from "./issue-markdown-field";

type CommentComposerProps = {
  busy: boolean;
  dictionary: Dictionary;
  issue: Issue;
  onSubmit: (body: string, nextState: "open" | "closed" | null) => Promise<boolean>;
  viewer: AuthUser;
};

export function CommentComposer(props: CommentComposerProps) {
  const copy = props.dictionary.issueDetail;
  const [body, setBody] = useState("");
  const nextState = props.issue.state === "open" ? "closed" : "open";
  const hasBody = body.trim().length > 0;

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!hasBody) return;
    if (await props.onSubmit(body, null)) setBody("");
  }

  async function submitWithState() {
    if (await props.onSubmit(hasBody ? body : "", nextState)) setBody("");
  }

  return (
    <div className={styles.timelineItem}>
      <span className={styles.gutter}>
        <UserAvatar avatarUrl={props.viewer.avatarUrl} name={viewerName(props.viewer)} size={40} />
      </span>
      <form className={styles.composer} onSubmit={submit}>
        <MarkdownField
          dictionary={props.dictionary}
          id="new-issue-comment"
          label={copy.addComment}
          onChange={setBody}
          placeholder={copy.commentPlaceholder}
          value={body}
        />
        <div className={styles.actions}>
          {props.issue.viewerCanUpdate && (
            <button
              className={styles.stateButton}
              data-state={props.issue.state}
              disabled={props.busy}
              onClick={submitWithState}
              type="button"
            >
              {stateButtonLabel(copy, props.issue.state, hasBody)}
            </button>
          )}
          <button className={styles.primaryButton} disabled={props.busy || !hasBody} type="submit">
            {copy.submitComment}
          </button>
        </div>
      </form>
    </div>
  );
}

function viewerName(viewer: AuthUser): string {
  return viewer.displayName || viewer.username;
}

function stateButtonLabel(copy: Dictionary["issueDetail"], state: Issue["state"], hasBody: boolean): string {
  if (state === "open") return hasBody ? copy.closeWithComment : copy.closeIssue;
  return hasBody ? copy.reopenWithComment : copy.reopenIssue;
}
