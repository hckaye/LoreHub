"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";

import styles from "@/components/create/create-form.module.css";
import { FlashNotice } from "@/components/ui/flash-notice";
import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, OrganizationView, Repository } from "@/lib/api-types";
import { postJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { loginUrl, repositoryPath } from "@/lib/routes";

type RegisterRepositoryFormProps = {
  locale: Locale;
  dictionary: Dictionary;
  organizations: OrganizationView[];
  session: Extract<AuthSession, { status: "authenticated" }>;
};

export function RegisterRepositoryForm({ dictionary, locale, organizations, session }: RegisterRepositoryFormProps) {
  const router = useRouter();
  const copy = dictionary.createPages;
  const [organization, setOrganization] = useState(organizations[0]?.slug ?? "");
  const [slug, setSlug] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [description, setDescription] = useState("");
  const [visibility, setVisibility] = useState<Repository["visibility"]>("public");
  const [failure, setFailure] = useState<string | null>(null);
  const [requiresLogin, setRequiresLogin] = useState(false);
  const [pending, setPending] = useState(false);

  if (organizations.length === 0) {
    return (
      <div className={styles.empty}>
        <FlashNotice body={copy.noOrganizationsBody} title={copy.noOrganizationsTitle} tone="warning" />
        <Link className={styles.primaryButton} href={`/${locale}/organizations/new`}>
          {dictionary.common.newOrganization}
        </Link>
      </div>
    );
  }

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
        displayName: displayName.trim() || slug.trim(),
        description,
        visibility,
      },
      session.csrfToken,
    );
    if (result.ok) {
      router.push(repositoryPath(locale, result.data.owner, result.data.slug));
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
        <Link className={styles.cancel} href={loginUrl(`/${locale}/repositories/new`)}>
          {dictionary.auth.loginToContinue}
        </Link>
      )}
      <div className={styles.ownerRow}>
        <div className={styles.field}>
          <label htmlFor="repository-organization">{copy.owner}</label>
          <select
            id="repository-organization"
            onChange={(event) => setOrganization(event.target.value)}
            required
            value={organization}
          >
            {organizations.map((item) => (
              <option key={item.id} value={item.slug}>
                {item.slug}
              </option>
            ))}
          </select>
        </div>
        <span aria-hidden="true" className={styles.slash}>
          /
        </span>
        <div className={styles.field}>
          <label htmlFor="repository-slug">{copy.repositoryName}</label>
          <input id="repository-slug" onChange={(event) => setSlug(event.target.value)} required value={slug} />
        </div>
      </div>
      <p className={styles.hint}>{copy.repositoryNameHelp}</p>
      <div className={styles.field}>
        <label htmlFor="repository-name">{dictionary.forms.displayName}</label>
        <input id="repository-name" onChange={(event) => setDisplayName(event.target.value)} value={displayName} />
      </div>
      <div className={styles.field}>
        <label htmlFor="repository-description">{copy.descriptionOptional}</label>
        <input
          id="repository-description"
          onChange={(event) => setDescription(event.target.value)}
          value={description}
        />
      </div>
      <fieldset className={styles.visibility}>
        <legend>{copy.visibilityLegend}</legend>
        <VisibilityChoice
          checked={visibility === "public"}
          help={copy.publicHelp}
          label={dictionary.common.public}
          onChange={() => setVisibility("public")}
          value="public"
        />
        <VisibilityChoice
          checked={visibility === "internal"}
          help={copy.internalHelp}
          label={dictionary.common.internal}
          onChange={() => setVisibility("internal")}
          value="internal"
        />
        <VisibilityChoice
          checked={visibility === "private"}
          help={copy.privateHelp}
          label={dictionary.common.private}
          onChange={() => setVisibility("private")}
          value="private"
        />
      </fieldset>
      <p className={styles.note}>{dictionary.forms.managedRepositoryDescription}</p>
      <div className={styles.actions}>
        <button className={styles.primaryButton} disabled={pending} type="submit">
          {pending ? dictionary.forms.submittingLabel : dictionary.forms.createRepository}
        </button>
      </div>
    </form>
  );
}

function VisibilityChoice({
  checked,
  help,
  label,
  onChange,
  value,
}: {
  checked: boolean;
  help: string;
  label: string;
  onChange: () => void;
  value: string;
}) {
  return (
    <label className={styles.choice}>
      <input checked={checked} name="repository-visibility" onChange={onChange} type="radio" value={value} />
      <span>
        <strong>{label}</strong>
        <small>{help}</small>
      </span>
    </label>
  );
}
