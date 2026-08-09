"use client";

import Link from "next/link";
import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Organization } from "@/lib/api-types";
import { postJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { loginUrl } from "@/lib/routes";

import styles from "../repositories/repository-form.module.css";
import { FlashNotice } from "../ui/flash-notice";

type OrganizationFormProps = {
  locale: Locale;
  dictionary: Dictionary;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

export function OrganizationForm({ dictionary, locale, session }: OrganizationFormProps) {
  const [slug, setSlug] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [description, setDescription] = useState("");
  const [visibility, setVisibility] = useState<Organization["visibility"]>("public");
  const [created, setCreated] = useState<Organization | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [requiresLogin, setRequiresLogin] = useState(false);
  const [pending, setPending] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!session.csrfToken) {
      setFailure(dictionary.auth.csrfMissing);
      setRequiresLogin(false);
      return;
    }
    setFailure(null);
    setRequiresLogin(false);
    setPending(true);
    const result = await postJson<Organization>(
      "/api/v1/organizations",
      { slug: slug.trim(), displayName: displayName.trim(), description, visibility },
      session.csrfToken,
    );
    if (result.ok) {
      setCreated(result.data);
      setPending(false);
      return;
    }
    setPending(false);
    setFailure(mutationFailureMessage(result.kind, dictionary));
    setRequiresLogin(result.kind === "unauthorized");
  }

  return (
    <div>
      {created && (
        <FlashNotice
          body={`${dictionary.forms.organizationCreated} ${created.displayName}`}
          title={dictionary.forms.organizationCreated}
          tone="success"
        />
      )}
      {requiresLogin && (
        <Link className={styles.cancel} href={loginUrl(`/${locale}/organizations/new`)}>
          {dictionary.auth.loginToContinue}
        </Link>
      )}
      <form className={styles.form} onSubmit={handleSubmit}>
        {failure && <FlashNotice body={failure} title={dictionary.forms.submitFailed} tone="error" />}
        <div className={styles.field}>
          <label htmlFor="organization-slug">{dictionary.forms.organizationSlug}</label>
          <input id="organization-slug" onChange={(event) => setSlug(event.target.value)} required value={slug} />
        </div>
        <div className={styles.field}>
          <label htmlFor="organization-name">{dictionary.forms.displayName}</label>
          <input
            id="organization-name"
            onChange={(event) => setDisplayName(event.target.value)}
            required
            value={displayName}
          />
        </div>
        <div className={styles.field}>
          <label htmlFor="organization-description">{dictionary.forms.description}</label>
          <textarea
            id="organization-description"
            onChange={(event) => setDescription(event.target.value)}
            value={description}
          />
        </div>
        <div className={styles.field}>
          <label htmlFor="organization-visibility">{dictionary.forms.visibility}</label>
          <select
            id="organization-visibility"
            onChange={(event) => setVisibility(event.target.value as Organization["visibility"])}
            value={visibility}
          >
            <option value="public">{dictionary.common.public}</option>
            <option value="internal">{dictionary.common.internal}</option>
            <option value="private">{dictionary.common.private}</option>
          </select>
        </div>
        <div className={styles.actions}>
          <button className={styles.submit} disabled={pending} type="submit">
            {pending ? dictionary.forms.submittingLabel : dictionary.forms.createOrganization}
          </button>
        </div>
      </form>
    </div>
  );
}
