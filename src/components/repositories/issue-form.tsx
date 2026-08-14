"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";

import { FlashNotice } from "@/components/ui/flash-notice";
import { UserAvatar } from "@/components/ui/user-avatar";
import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Issue } from "@/lib/api-types";
import { postJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { localizedPath, loginUrl, repositoryPath } from "@/lib/routes";
import { validateTitleAndBody } from "@/lib/validation";

import styles from "./issue-form.module.css";
import { MarkdownField } from "./issue-markdown-field";

type IssueFormProps = {
  locale: Locale;
  owner: string;
  repository: string;
  dictionary: Dictionary;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

export function IssueForm({ locale, owner, repository, dictionary, session }: IssueFormProps) {
  const router = useRouter();
  const copy = dictionary.createPages;
  const issuesPath = repositoryPath(locale, owner, repository, "issues");
  const viewerName = session.user.displayName || session.user.username;
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
      router.push(`${issuesPath}?created=1`);
      router.refresh();
      return;
    }
    setPending(false);
    setFailure(mutationFailureMessage(result.kind, dictionary));
    setRequiresLogin(result.kind === "unauthorized");
  }

  return (
    <form className={styles.page} onSubmit={handleSubmit}>
      <div className={styles.layout}>
        <div className={styles.main}>
          {(failure || requiresLogin || errors.length > 0) && (
            <div className={styles.notices}>
              {failure && <FlashNotice body={failure} title={dictionary.forms.submitFailed} tone="error" />}
              {requiresLogin && (
                <Link className={styles.loginLink} href={loginUrl(issuesPath)}>
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
            </div>
          )}
          <div className={styles.commentRow}>
            <Link
              aria-hidden="true"
              className={styles.avatar}
              href={localizedPath(locale, session.user.username)}
              tabIndex={-1}
            >
              <UserAvatar avatarUrl={session.user.avatarUrl} name={viewerName} size={40} />
            </Link>
            <div className={styles.formColumn}>
              <label className="visually-hidden" htmlFor="issue-title">
                {dictionary.forms.titleLabel}
              </label>
              <input
                aria-describedby={errors.length > 0 ? "issue-errors" : undefined}
                className={styles.title}
                id="issue-title"
                maxLength={512}
                onChange={(event) => setTitle(event.target.value)}
                placeholder={dictionary.forms.titleLabel}
                required
                value={title}
              />
              <MarkdownField
                dictionary={dictionary}
                id="issue-body"
                label={dictionary.forms.bodyLabel}
                labelHidden
                onChange={setBody}
                placeholder={dictionary.forms.bodyPlaceholder}
                value={body}
                variant="commentBox"
              />
              <div className={styles.actions}>
                <button className={styles.submit} disabled={pending} type="submit">
                  {pending ? dictionary.forms.submittingLabel : dictionary.forms.submitIssue}
                </button>
              </div>
            </div>
          </div>
        </div>
        <aside className={styles.sidebar}>
          <section>
            <h2>{dictionary.issueAssignees.title}</h2>
            <p>{copy.noneYet}</p>
          </section>
          <section>
            <h2>{dictionary.issueDetail.labels}</h2>
            <p>{copy.noneYet}</p>
          </section>
          <section>
            <h2>{dictionary.issueDetail.milestone}</h2>
            <p>{copy.noneYet}</p>
          </section>
        </aside>
      </div>
    </form>
  );
}
