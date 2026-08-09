"use client";

import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, Repository } from "@/lib/api-types";
import { patchJson } from "@/lib/auth-client";

import styles from "./repository-settings-form.module.css";

type RepositorySettingsFormProps = {
  dictionary: Dictionary;
  repository: Repository;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

export function RepositorySettingsForm({ dictionary, repository, session }: RepositorySettingsFormProps) {
  const [displayName, setDisplayName] = useState(repository.displayName);
  const [description, setDescription] = useState(repository.description);
  const [homepageUrl, setHomepageUrl] = useState(repository.homepageUrl);
  const [visibility, setVisibility] = useState(repository.visibility);
  const [status, setStatus] = useState<"idle" | "saved" | "error">("idle");
  const [pending, setPending] = useState(false);

  async function save(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPending(true);
    setStatus("idle");
    const result = await patchJson<Repository>(
      `/api/v1/repositories/${encodeURIComponent(repository.owner)}/${encodeURIComponent(repository.slug)}/settings`,
      { displayName, description, homepageUrl, visibility },
      session.csrfToken,
    );
    setPending(false);
    setStatus(result.ok ? "saved" : "error");
  }

  return (
    <form className={styles.form} onSubmit={save}>
      <label>
        <span>{dictionary.settingsPage.displayName}</span>
        <input onChange={(event) => setDisplayName(event.target.value)} required value={displayName} />
      </label>
      <label>
        <span>{dictionary.settingsPage.descriptionLabel}</span>
        <textarea onChange={(event) => setDescription(event.target.value)} rows={4} value={description} />
      </label>
      <label>
        <span>{dictionary.profile.website}</span>
        <input onChange={(event) => setHomepageUrl(event.target.value)} value={homepageUrl} />
      </label>
      <label>
        <span>{dictionary.settingsPage.visibility}</span>
        <select onChange={(event) => setVisibility(event.target.value as typeof visibility)} value={visibility}>
          <option value="private">{dictionary.common.private}</option>
          <option value="internal">{dictionary.common.internal}</option>
          <option value="public">{dictionary.common.public}</option>
        </select>
      </label>
      <div className={styles.actions}>
        <button disabled={pending} type="submit">
          {pending ? dictionary.common.loading : dictionary.settingsPage.saveSettings}
        </button>
        {status === "saved" && <span role="status">{dictionary.settingsPage.settingsSaved}</span>}
        {status === "error" && <span role="alert">{dictionary.forms.submitFailed}</span>}
      </div>
    </form>
  );
}
