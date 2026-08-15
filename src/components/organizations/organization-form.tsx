"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";

import styles from "@/components/create/create-form.module.css";
import { FlashNotice } from "@/components/ui/flash-notice";
import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Organization } from "@/lib/api-types";
import { postJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { loginUrl } from "@/lib/routes";

type OrganizationFormProps = {
  locale: Locale;
  dictionary: Dictionary;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

export function OrganizationForm({ dictionary, locale, session }: OrganizationFormProps) {
  const router = useRouter();
  const copy = dictionary.createPages;
  const [slug, setSlug] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [description, setDescription] = useState("");
  const [visibility, setVisibility] = useState<Organization["visibility"]>("public");
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
      { slug: slug.trim(), displayName: displayName.trim() || slug.trim(), description, visibility },
      session.csrfToken,
    );
    if (result.ok) {
      router.push(`/${locale}/organizations/${encodeURIComponent(result.data.slug)}`);
      router.refresh();
      return;
    }
    setPending(false);
    setFailure(mutationFailureMessage(result.kind, dictionary, result.code));
    setRequiresLogin(result.kind === "unauthorized");
  }

  return (
    <form className={styles.form} onSubmit={handleSubmit}>
      {failure && <FlashNotice body={failure} title={dictionary.forms.submitFailed} tone="error" />}
      {requiresLogin && (
        <Link className={styles.cancel} href={loginUrl(`/${locale}/organizations/new`)}>
          {dictionary.auth.loginToContinue}
        </Link>
      )}
      <div className={styles.field}>
        <label htmlFor="organization-slug">{copy.organizationName}</label>
        <input id="organization-slug" onChange={(event) => setSlug(event.target.value)} required value={slug} />
        <p className={styles.hint}>{copy.organizationNameHelp}</p>
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
        <label htmlFor="organization-description">{copy.descriptionOptional}</label>
        <input
          id="organization-description"
          onChange={(event) => setDescription(event.target.value)}
          value={description}
        />
      </div>
      <fieldset className={styles.visibility}>
        <legend>{copy.visibilityLegend}</legend>
        <label className={styles.choice}>
          <input
            checked={visibility === "public"}
            name="organization-visibility"
            onChange={() => setVisibility("public")}
            type="radio"
            value="public"
          />
          <span>
            <strong>{dictionary.common.public}</strong>
            <small>{copy.organizationPublicHelp}</small>
          </span>
        </label>
        <label className={styles.choice}>
          <input
            checked={visibility === "internal"}
            name="organization-visibility"
            onChange={() => setVisibility("internal")}
            type="radio"
            value="internal"
          />
          <span>
            <strong>{dictionary.common.internal}</strong>
            <small>{copy.organizationInternalHelp}</small>
          </span>
        </label>
        <label className={styles.choice}>
          <input
            checked={visibility === "private"}
            name="organization-visibility"
            onChange={() => setVisibility("private")}
            type="radio"
            value="private"
          />
          <span>
            <strong>{dictionary.common.private}</strong>
            <small>{copy.organizationPrivateHelp}</small>
          </span>
        </label>
      </fieldset>
      <div className={styles.actions}>
        <button className={styles.primaryButton} disabled={pending} type="submit">
          {pending ? dictionary.forms.submittingLabel : dictionary.forms.createOrganization}
        </button>
      </div>
    </form>
  );
}
