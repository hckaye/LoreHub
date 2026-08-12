"use client";

import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, DiscussionCategory } from "@/lib/api-types";
import { deleteJson, patchJson, postJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";

import styles from "./discussion-category-settings.module.css";

type CategorySettingsProps = {
  categories: DiscussionCategory[];
  dictionary: Dictionary;
  owner: string;
  repository: string;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

type CategoryDraft = {
  slug: string;
  name: string;
  description: string;
  format: DiscussionCategory["format"];
};

const emptyDraft: CategoryDraft = { slug: "", name: "", description: "", format: "discussion" };

export function DiscussionCategorySettings(props: CategorySettingsProps) {
  const copy = props.dictionary.discussionsPage;
  const [categories, setCategories] = useState(props.categories);
  const [draft, setDraft] = useState<CategoryDraft>(emptyDraft);
  const [editing, setEditing] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const base = `/api/v1/repositories/${encodeURIComponent(props.owner)}/${encodeURIComponent(
    props.repository,
  )}/discussions/categories`;

  function startEdit(category: DiscussionCategory) {
    setEditing(category.slug);
    setDraft({
      slug: category.slug,
      name: category.name,
      description: category.description,
      format: category.format,
    });
    setError("");
    setNotice("");
  }

  function reset() {
    setEditing(null);
    setDraft(emptyDraft);
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    setNotice("");
    const result = editing
      ? await patchJson<DiscussionCategory>(`${base}/${encodeURIComponent(editing)}`, draft, props.session.csrfToken)
      : await postJson<DiscussionCategory>(base, draft, props.session.csrfToken);
    setBusy(false);
    if (!result.ok) {
      setError(mutationFailureMessage(result.kind, props.dictionary));
      return;
    }
    setCategories((current) =>
      editing ? current.map((item) => (item.id === result.data.id ? result.data : item)) : [...current, result.data],
    );
    setNotice(copy.categorySaved);
    reset();
  }

  async function remove(category: DiscussionCategory) {
    if (!window.confirm(copy.deleteCategoryConfirm)) return;
    setBusy(true);
    setError("");
    setNotice("");
    const result = await deleteJson<null>(`${base}/${encodeURIComponent(category.slug)}`, props.session.csrfToken);
    setBusy(false);
    if (!result.ok) {
      setError(mutationFailureMessage(result.kind, props.dictionary));
      return;
    }
    setCategories((current) => current.filter((item) => item.id !== category.id));
    setNotice(copy.categoryDeleted);
  }

  return (
    <div className={styles.stack}>
      {error && (
        <p className={styles.error} role="alert">
          {error}
        </p>
      )}
      {notice && (
        <p className={styles.success} role="status">
          {notice}
        </p>
      )}
      <form className={styles.form} onSubmit={save}>
        <div className={styles.fields}>
          <div className={styles.field}>
            <label htmlFor="discussion-category-slug">{copy.slug}</label>
            <input
              disabled={editing !== null}
              id="discussion-category-slug"
              maxLength={64}
              onChange={(event) => setDraft((current) => ({ ...current, slug: event.target.value }))}
              required
              value={draft.slug}
            />
          </div>
          <div className={styles.field}>
            <label htmlFor="discussion-category-name">{copy.name}</label>
            <input
              id="discussion-category-name"
              maxLength={100}
              onChange={(event) => setDraft((current) => ({ ...current, name: event.target.value }))}
              required
              value={draft.name}
            />
          </div>
          <div className={styles.field}>
            <label htmlFor="discussion-category-format">{copy.format}</label>
            <select
              id="discussion-category-format"
              onChange={(event) =>
                setDraft((current) => ({ ...current, format: event.target.value as CategoryDraft["format"] }))
              }
              value={draft.format}
            >
              <option value="discussion">{copy.discussion}</option>
              <option value="question">{copy.question}</option>
              <option value="announcement">{copy.announcement}</option>
            </select>
          </div>
        </div>
        <div className={styles.field}>
          <label htmlFor="discussion-category-description">{copy.descriptionLabel}</label>
          <textarea
            id="discussion-category-description"
            maxLength={500}
            onChange={(event) => setDraft((current) => ({ ...current, description: event.target.value }))}
            rows={2}
            value={draft.description}
          />
        </div>
        <div className={styles.actions}>
          <button className={styles.button} disabled={busy} type="submit">
            {editing ? copy.saveCategory : copy.createCategory}
          </button>
          {editing && (
            <button className={styles.secondaryButton} onClick={reset} type="button">
              {copy.cancel}
            </button>
          )}
        </div>
      </form>
      <div className={styles.list}>
        {categories.map((category) => (
          <div className={styles.item} key={category.id}>
            <div>
              <strong>{category.name}</strong> <span>({category.slug})</span>
              <p>{category.description || copy.noDescription}</p>
            </div>
            <div className={styles.actions}>
              <button
                className={styles.secondaryButton}
                disabled={busy}
                onClick={() => startEdit(category)}
                type="button"
              >
                {copy.editCategory}
              </button>
              <button
                className={styles.dangerButton}
                disabled={busy}
                onClick={() => void remove(category)}
                type="button"
              >
                {copy.deleteCategory}
              </button>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
