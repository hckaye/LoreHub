"use client";

import Link from "next/link";
import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Repository } from "@/lib/api-types";
import { postJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { loginUrl } from "@/lib/routes";

import { FlashNotice } from "../ui/flash-notice";
import styles from "./repository-form.module.css";

type RegisterRepositoryFormProps = {
  locale: Locale;
  dictionary: Dictionary;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

export function RegisterRepositoryForm({ dictionary, locale, session }: RegisterRepositoryFormProps) {
  const [organization, setOrganization] = useState("");
  const [slug, setSlug] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [description, setDescription] = useState("");
  const [visibility, setVisibility] = useState<Repository["visibility"]>("public");
  const [created, setCreated] = useState<Repository | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [requiresLogin, setRequiresLogin] = useState(false);
  const [pending, setPending] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!organization.trim() || !slug.trim()) {
      setFailure(dictionary.forms.organizationRequired);
      setRequiresLogin(false);
      return;
    }
    if (!session.csrfToken) {
      setFailure(dictionary.auth.csrfMissing);
      setRequiresLogin(false);
      return;
    }
    setFailure(null);
    setRequiresLogin(false);
    setPending(true);
    const result = await postJson<Repository>(
      `/api/v1/organizations/${encodeURIComponent(organization.trim())}/repositories`,
      {
        slug: slug.trim(),
        displayName: displayName.trim(),
        description,
        visibility,
      },
      session.csrfToken,
    );
    if (result.ok) {
      setCreated(result.data);
      setPending(false);
      return;
    }
    setPending(false);
    setFailure(mutationFailureMessage(result.kind, dictionary, result.code));
    setRequiresLogin(result.kind === "unauthorized");
  }

  return (
    <div>
      {created && (
        <FlashNotice
          body={`${dictionary.forms.repositoryCreated} ${created.owner}/${created.slug}`}
          title={dictionary.forms.repositoryCreated}
          tone="success"
        />
      )}
      {requiresLogin && (
        <Link className={styles.cancel} href={loginUrl(`/${locale}/repositories/new`)}>
          {dictionary.auth.loginToContinue}
        </Link>
      )}
      <form className={styles.form} onSubmit={handleSubmit}>
        <p>{dictionary.forms.managedRepositoryDescription}</p>
        {failure && <FlashNotice body={failure} title={dictionary.forms.submitFailed} tone="error" />}
        <div className={styles.field}>
          <label htmlFor="repository-organization">{dictionary.forms.organizationSlug}</label>
          <input
            id="repository-organization"
            onChange={(event) => setOrganization(event.target.value)}
            required
            value={organization}
          />
        </div>
        <div className={styles.field}>
          <label htmlFor="repository-slug">{dictionary.forms.repositorySlug}</label>
          <input id="repository-slug" onChange={(event) => setSlug(event.target.value)} required value={slug} />
        </div>
        <div className={styles.field}>
          <label htmlFor="repository-name">{dictionary.forms.displayName}</label>
          <input id="repository-name" onChange={(event) => setDisplayName(event.target.value)} value={displayName} />
        </div>
        <div className={styles.field}>
          <label htmlFor="repository-description">{dictionary.forms.description}</label>
          <textarea
            id="repository-description"
            onChange={(event) => setDescription(event.target.value)}
            value={description}
          />
        </div>
        <div className={styles.field}>
          <label htmlFor="repository-visibility">{dictionary.forms.visibility}</label>
          <select
            id="repository-visibility"
            onChange={(event) => setVisibility(event.target.value as Repository["visibility"])}
            value={visibility}
          >
            <option value="public">{dictionary.common.public}</option>
            <option value="internal">{dictionary.common.internal}</option>
            <option value="private">{dictionary.common.private}</option>
          </select>
        </div>
        <div className={styles.actions}>
          <button className={styles.submit} disabled={pending} type="submit">
            {pending ? dictionary.forms.submittingLabel : dictionary.forms.createRepository}
          </button>
        </div>
      </form>
    </div>
  );
}
