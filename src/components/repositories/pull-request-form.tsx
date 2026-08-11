"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Branch, MergeRequest } from "@/lib/api-types";
import { postJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { loginUrl, repositoryPath } from "@/lib/routes";
import { validateBranchSelection, validateTitleAndBody } from "@/lib/validation";

import { FlashNotice } from "../ui/flash-notice";
import styles from "./repository-form.module.css";

type PullRequestFormProps = {
  locale: Locale;
  owner: string;
  repository: string;
  dictionary: Dictionary;
  session: Extract<AuthSession, { status: "authenticated" }>;
  branches: Branch[];
};

export function PullRequestForm({ locale, owner, repository, dictionary, session, branches }: PullRequestFormProps) {
  const router = useRouter();
  const usableBranches = branches.filter((branch) => !branch.archived);
  const initialTarget = usableBranches.find((branch) => branch.current) ?? usableBranches[0];
  const initialSource = usableBranches.find((branch) => branch.id !== initialTarget?.id) ?? usableBranches[0];
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [sourceBranch, setSourceBranch] = useState(initialSource?.name ?? "");
  const [targetBranch, setTargetBranch] = useState(initialTarget?.name ?? "");
  const [isDraft, setIsDraft] = useState(false);
  const [errors, setErrors] = useState<string[]>([]);
  const [failure, setFailure] = useState<string | null>(null);
  const [requiresLogin, setRequiresLogin] = useState(false);
  const [pending, setPending] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const validationErrors = [
      ...validateTitleAndBody(title, body),
      ...validateBranchSelection(sourceBranch, targetBranch),
    ];
    if (validationErrors.length > 0) {
      setErrors(
        validationErrors.map((error) => {
          if (error === "titleRequired") return dictionary.forms.titleRequired;
          if (error === "titleTooLong") return dictionary.forms.titleTooLong;
          if (error === "bodyTooLong") return dictionary.forms.bodyTooLong;
          return dictionary.forms.branchesSame;
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
    const result = await postJson<MergeRequest>(
      `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/merge-requests`,
      { title: title.trim(), body, isDraft, sourceBranch, targetBranch },
      session.csrfToken,
    );
    if (result.ok) {
      router.push(`${repositoryPath(locale, owner, repository, "pulls")}?created=1`);
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
        <Link className={styles.cancel} href={loginUrl(repositoryPath(locale, owner, repository, "pulls"))}>
          {dictionary.auth.loginToContinue}
        </Link>
      )}
      {errors.length > 0 && (
        <div aria-live="polite" className={styles.error} id="pull-request-errors" role="alert">
          {errors.map((error) => (
            <p key={error}>{error}</p>
          ))}
        </div>
      )}
      <div className={styles.field}>
        <label htmlFor="pull-request-title">{dictionary.forms.titleLabel}</label>
        <input
          aria-describedby={errors.length > 0 ? "pull-request-errors" : undefined}
          id="pull-request-title"
          maxLength={512}
          onChange={(event) => setTitle(event.target.value)}
          placeholder={dictionary.forms.titlePlaceholder}
          required
          value={title}
        />
      </div>
      <div className={styles.field}>
        <label htmlFor="pull-request-body">{dictionary.forms.bodyLabel}</label>
        <textarea
          id="pull-request-body"
          maxLength={1_000_000}
          onChange={(event) => setBody(event.target.value)}
          placeholder={dictionary.forms.bodyPlaceholder}
          value={body}
        />
      </div>
      <div className={styles.field}>
        <label htmlFor="pull-request-source">{dictionary.forms.sourceBranch}</label>
        <select id="pull-request-source" onChange={(event) => setSourceBranch(event.target.value)} value={sourceBranch}>
          {usableBranches.map((branch) => (
            <option key={branch.id} value={branch.name}>
              {branch.name}
            </option>
          ))}
        </select>
      </div>
      <div className={styles.field}>
        <label htmlFor="pull-request-target">{dictionary.forms.targetBranch}</label>
        <select id="pull-request-target" onChange={(event) => setTargetBranch(event.target.value)} value={targetBranch}>
          {usableBranches.map((branch) => (
            <option key={branch.id} value={branch.name}>
              {branch.name}
            </option>
          ))}
        </select>
      </div>
      <label className={styles.checkbox}>
        <input checked={isDraft} onChange={(event) => setIsDraft(event.target.checked)} type="checkbox" />
        <span>
          <strong>{dictionary.pullRequestDrafts.createAsDraft}</strong>
          <small>{dictionary.pullRequestDrafts.createAsDraftHelp}</small>
        </span>
      </label>
      <div className={styles.actions}>
        <button className={styles.submit} disabled={pending} type="submit">
          {pending
            ? dictionary.forms.submittingLabel
            : isDraft
              ? dictionary.pullRequestDrafts.createDraft
              : dictionary.forms.submitPullRequest}
        </button>
        <a className={styles.cancel} href={repositoryPath(locale, owner, repository, "pulls")}>
          {dictionary.common.cancel}
        </a>
      </div>
    </form>
  );
}
