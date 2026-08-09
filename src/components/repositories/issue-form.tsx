"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Issue } from "@/lib/api-types";
import { postJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { loginUrl, repositoryPath } from "@/lib/routes";
import { validateTitleAndBody } from "@/lib/validation";

import { FlashNotice } from "../ui/flash-notice";
import styles from "./repository-form.module.css";

type IssueFormProps = {
  locale: Locale;
  owner: string;
  repository: string;
  dictionary: Dictionary;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

export function IssueForm({ locale, owner, repository, dictionary, session }: IssueFormProps) {
  const router = useRouter();
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [errors, setErrors] = useState<string[]>([]);
  const [failure, setFailure] = useState<string | null>(null);
  const [requiresLogin, setRequiresLogin] = useState(false);
  const [pending, setPending] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const validationErrors = validateTitleAndBody(title, body);
    if (validationErrors.length > 0) {
      setErrors(
        validationErrors.map((error) => {
          if (error === "titleRequired") return dictionary.forms.titleRequired;
          if (error === "titleTooLong") return dictionary.forms.titleTooLong;
          return dictionary.forms.bodyTooLong;
        }),
      );
      setFailure(null);
      setRequiresLogin(false);
      return;
    }
    if (!session.csrfToken) {
      setFailure(dictionary.auth.csrfMissing);
      setRequiresLogin(false);
      return;
    }
    setErrors([]);
    setFailure(null);
    setRequiresLogin(false);
    setPending(true);
    const result = await postJson<Issue>(
      `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/issues`,
      { title: title.trim(), body },
      session.csrfToken,
    );
    if (result.ok) {
      router.push(`${repositoryPath(locale, owner, repository, "issues")}?created=1`);
      router.refresh();
      return;
    }
    setPending(false);
    setFailure(mutationFailureMessage(result.kind, dictionary));
    setRequiresLogin(result.kind === "unauthorized");
  }

  return (
    <form className={styles.form} onSubmit={handleSubmit}>
      {failure && <FlashNotice body={failure} title={dictionary.forms.submitFailed} tone="error" />}
      {requiresLogin && (
        <Link className={styles.cancel} href={loginUrl(repositoryPath(locale, owner, repository, "issues"))}>
          {dictionary.auth.loginToContinue}
        </Link>
      )}
      {errors.length > 0 && (
        <div aria-live="polite" className={styles.error} id="issue-errors" role="alert">
          {errors.map((error) => (
            <p key={error}>{error}</p>
          ))}
        </div>
      )}
      <div className={styles.field}>
        <label htmlFor="issue-title">{dictionary.forms.titleLabel}</label>
        <input
          aria-describedby={errors.length > 0 ? "issue-errors" : undefined}
          id="issue-title"
          maxLength={512}
          onChange={(event) => setTitle(event.target.value)}
          placeholder={dictionary.forms.titlePlaceholder}
          required
          value={title}
        />
      </div>
      <div className={styles.field}>
        <label htmlFor="issue-body">{dictionary.forms.bodyLabel}</label>
        <textarea
          id="issue-body"
          maxLength={1_000_000}
          onChange={(event) => setBody(event.target.value)}
          placeholder={dictionary.forms.bodyPlaceholder}
          value={body}
        />
      </div>
      <div className={styles.actions}>
        <button className={styles.submit} disabled={pending} type="submit">
          {pending ? dictionary.forms.submittingLabel : dictionary.forms.submitIssue}
        </button>
        <a className={styles.cancel} href={repositoryPath(locale, owner, repository, "issues")}>
          {dictionary.common.cancel}
        </a>
      </div>
    </form>
  );
}
