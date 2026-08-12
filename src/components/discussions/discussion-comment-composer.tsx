"use client";

import { FormEvent, useState } from "react";

import type { DiscussionCopy } from "./discussion-component-types";
import styles from "./discussion-detail.module.css";

type DiscussionCommentComposerProps = {
  busy: boolean;
  copy: DiscussionCopy;
  onSubmit: (body: string) => Promise<boolean>;
};

export function DiscussionCommentComposer(props: DiscussionCommentComposerProps) {
  const [body, setBody] = useState("");

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (await props.onSubmit(body)) setBody("");
  }

  return (
    <form className={styles.editor} onSubmit={submit}>
      <label htmlFor="discussion-comment">{props.copy.submitReply}</label>
      <textarea
        id="discussion-comment"
        maxLength={262_144}
        onChange={(event) => setBody(event.target.value)}
        required
        value={body}
      />
      <button className={styles.button} disabled={props.busy} type="submit">
        {props.copy.submitReply}
      </button>
    </form>
  );
}
