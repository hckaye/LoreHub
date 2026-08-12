"use client";

import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";

import styles from "./revision-comments.module.css";

type RevisionCommentComposerProps = {
  busy: boolean;
  dictionary: Dictionary;
  onSubmit: (body: string) => Promise<boolean>;
};

export function RevisionCommentComposer(props: RevisionCommentComposerProps) {
  const [body, setBody] = useState("");
  const copy = props.dictionary.revisionComments;
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (await props.onSubmit(body)) setBody("");
  }
  return (
    <form className={styles.composer} onSubmit={submit}>
      <label htmlFor="new-revision-comment">{copy.add}</label>
      <textarea
        id="new-revision-comment"
        maxLength={1_000_000}
        onChange={(event) => setBody(event.target.value)}
        placeholder={copy.placeholder}
        required
        value={body}
      />
      <div className={styles.formActions}>
        <button disabled={props.busy || body.trim() === ""} type="submit">
          {copy.submit}
        </button>
      </div>
    </form>
  );
}
