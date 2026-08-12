"use client";

import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { DiscussionCategory } from "@/lib/api-types";

import styles from "./discussion-detail.module.css";

type DiscussionEditorProps = {
  body: string;
  busy: boolean;
  categories: DiscussionCategory[];
  category: string;
  copy: Dictionary["discussionsPage"];
  onCancel: () => void;
  onSave: (input: Record<string, unknown>) => Promise<boolean>;
  title: string;
  viewerCanModerate: boolean;
};

export function DiscussionEditor(props: DiscussionEditorProps) {
  const [category, setCategory] = useState(props.category);
  const [title, setTitle] = useState(props.title);
  const [body, setBody] = useState(props.body);
  const categories = props.categories.filter(
    (item) => item.format !== "announcement" || props.viewerCanModerate || item.slug === props.category,
  );
  return (
    <form
      className={styles.editor}
      onSubmit={(event) => {
        event.preventDefault();
        void props.onSave({ category, title: title.trim(), body });
      }}
    >
      <label htmlFor="discussion-edit-category">{props.copy.categoryLabel}</label>
      <select id="discussion-edit-category" onChange={(event) => setCategory(event.target.value)} value={category}>
        {categories.map((item) => (
          <option key={item.id} value={item.slug}>
            {item.name}
          </option>
        ))}
      </select>
      <label htmlFor="discussion-edit-title">{props.copy.titleLabel}</label>
      <input
        id="discussion-edit-title"
        maxLength={512}
        onChange={(event) => setTitle(event.target.value)}
        required
        value={title}
      />
      <label htmlFor="discussion-edit-body">{props.copy.bodyLabel}</label>
      <textarea
        id="discussion-edit-body"
        maxLength={1_000_000}
        onChange={(event) => setBody(event.target.value)}
        value={body}
      />
      <div className={styles.actions}>
        <button className={styles.button} disabled={props.busy} type="submit">
          {props.copy.save}
        </button>
        <button className={styles.secondaryButton} onClick={props.onCancel} type="button">
          {props.copy.cancel}
        </button>
      </div>
    </form>
  );
}
