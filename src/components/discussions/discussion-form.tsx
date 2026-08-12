"use client";

import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Discussion, DiscussionCategory } from "@/lib/api-types";
import { postJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { repositoryPath } from "@/lib/routes";

import styles from "./discussion-form.module.css";

type DiscussionFormProps = {
  categories: DiscussionCategory[];
  dictionary: Dictionary;
  locale: Locale;
  owner: string;
  repository: string;
  session: Extract<AuthSession, { status: "authenticated" }>;
  viewerCanModerate: boolean;
};

export function DiscussionForm(props: DiscussionFormProps) {
  const router = useRouter();
  const copy = props.dictionary.discussionsPage;
  const availableCategories = props.categories.filter(
    (category) => category.format !== "announcement" || props.viewerCanModerate,
  );
  const [category, setCategory] = useState(availableCategories[0]?.slug ?? "");
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    if (!category || !title.trim() || !body.trim()) {
      setError(props.dictionary.errors.invalid);
      return;
    }
    setPending(true);
    const result = await postJson<Discussion>(
      `/api/v1/repositories/${encodeURIComponent(props.owner)}/${encodeURIComponent(props.repository)}/discussions`,
      { category, title: title.trim(), body },
      props.session.csrfToken,
    );
    if (!result.ok) {
      setPending(false);
      setError(mutationFailureMessage(result.kind, props.dictionary));
      return;
    }
    router.push(`${repositoryPath(props.locale, props.owner, props.repository, "discussions")}/${result.data.number}`);
    router.refresh();
  }

  return (
    <form className={styles.form} onSubmit={submit}>
      {error && (
        <p className={styles.error} role="alert">
          {error}
        </p>
      )}
      <div className={styles.field}>
        <label htmlFor="discussion-category">{copy.categoryLabel}</label>
        <select
          id="discussion-category"
          onChange={(event) => setCategory(event.target.value)}
          required
          value={category}
        >
          {availableCategories.map((item) => (
            <option key={item.id} value={item.slug}>
              {item.name}
            </option>
          ))}
        </select>
        <p className={styles.hint}>{copy.formatHint}</p>
      </div>
      <div className={styles.field}>
        <label htmlFor="discussion-title">{copy.titleLabel}</label>
        <input
          id="discussion-title"
          maxLength={512}
          onChange={(event) => setTitle(event.target.value)}
          placeholder={copy.titlePlaceholder}
          required
          value={title}
        />
      </div>
      <div className={styles.field}>
        <label htmlFor="discussion-body">{copy.bodyLabel}</label>
        <textarea
          id="discussion-body"
          maxLength={1_000_000}
          onChange={(event) => setBody(event.target.value)}
          placeholder={copy.bodyPlaceholder}
          required
          value={body}
        />
      </div>
      <div className={styles.actions}>
        <button className={styles.submit} disabled={pending || availableCategories.length === 0} type="submit">
          {pending ? props.dictionary.common.loading : copy.create}
        </button>
        <a className={styles.cancel} href={repositoryPath(props.locale, props.owner, props.repository, "discussions")}>
          {props.dictionary.common.cancel}
        </a>
      </div>
    </form>
  );
}
