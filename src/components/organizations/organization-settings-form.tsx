"use client";

import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, OrganizationView } from "@/lib/api-types";
import { patchJson } from "@/lib/auth-client";

import styles from "./organization-settings-form.module.css";

type OrganizationSettingsFormProps = {
  dictionary: Dictionary;
  organization: OrganizationView;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

export function OrganizationSettingsForm({ dictionary, organization, session }: OrganizationSettingsFormProps) {
  const [displayName, setDisplayName] = useState(organization.displayName);
  const [description, setDescription] = useState(organization.description);
  const [websiteUrl, setWebsiteUrl] = useState(organization.websiteUrl);
  const [contactEmail, setContactEmail] = useState(organization.contactEmail);
  const [visibility, setVisibility] = useState(organization.visibility);
  const [defaultVisibility, setDefaultVisibility] = useState(organization.defaultRepositoryVisibility);
  const [status, setStatus] = useState<"idle" | "saved" | "error">("idle");
  const [pending, setPending] = useState(false);

  async function save(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPending(true);
    setStatus("idle");
    const result = await patchJson<OrganizationView>(
      `/api/v1/organizations/${encodeURIComponent(organization.slug)}/settings`,
      {
        displayName,
        description,
        websiteUrl,
        contactEmail,
        visibility,
        defaultRepositoryVisibility: defaultVisibility,
      },
      session.csrfToken,
    );
    setPending(false);
    setStatus(result.ok ? "saved" : "error");
  }

  return (
    <form className={styles.form} onSubmit={save}>
      <div className={styles.heading}>
        <div>
          <h2>{dictionary.organizationPage.settings}</h2>
          <p>{dictionary.accountSettings.profileBody}</p>
        </div>
      </div>
      <div className={styles.grid}>
        <label>
          <span>{dictionary.profile.displayName}</span>
          <input onChange={(event) => setDisplayName(event.target.value)} required value={displayName} />
        </label>
        <label>
          <span>{dictionary.organizationPage.website}</span>
          <input onChange={(event) => setWebsiteUrl(event.target.value)} value={websiteUrl} />
        </label>
        <label>
          <span>{dictionary.organizationPage.contactEmail}</span>
          <input onChange={(event) => setContactEmail(event.target.value)} type="email" value={contactEmail} />
        </label>
        <label>
          <span>{dictionary.organizationPage.defaultVisibility}</span>
          <select
            onChange={(event) => setDefaultVisibility(event.target.value as typeof defaultVisibility)}
            value={defaultVisibility}
          >
            <option value="private">{dictionary.common.private}</option>
            <option value="internal">{dictionary.common.internal}</option>
            <option value="public">{dictionary.common.public}</option>
          </select>
        </label>
      </div>
      <label>
        <span>{dictionary.forms.description}</span>
        <textarea onChange={(event) => setDescription(event.target.value)} rows={4} value={description} />
      </label>
      <label>
        <span>{dictionary.organizationPage.visibility}</span>
        <select onChange={(event) => setVisibility(event.target.value as typeof visibility)} value={visibility}>
          <option value="private">{dictionary.common.private}</option>
          <option value="internal">{dictionary.common.internal}</option>
          <option value="public">{dictionary.common.public}</option>
        </select>
      </label>
      <div className={styles.actions}>
        <button disabled={pending} type="submit">
          {pending ? dictionary.common.loading : dictionary.organizationPage.saveSettings}
        </button>
        {status === "saved" && <span role="status">{dictionary.organizationPage.settingsSaved}</span>}
        {status === "error" && <span role="alert">{dictionary.forms.submitFailed}</span>}
      </div>
    </form>
  );
}
