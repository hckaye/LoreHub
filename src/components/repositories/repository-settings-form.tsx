"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, Repository } from "@/lib/api-types";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { updateRepositorySettings } from "@/lib/repository-settings-client";

import styles from "./repository-settings-form.module.css";

type RepositorySettingsFormProps = {
  dictionary: Dictionary;
  repository: Repository;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

export function RepositorySettingsForm({ dictionary, repository, session }: RepositorySettingsFormProps) {
  const router = useRouter();
  const [displayName, setDisplayName] = useState(repository.displayName);
  const [description, setDescription] = useState(repository.description);
  const [homepageUrl, setHomepageUrl] = useState(repository.homepageUrl);
  const [visibility, setVisibility] = useState(repository.visibility);
  const [status, setStatus] = useState<"idle" | "saved" | "error">("idle");
  const [pending, setPending] = useState(false);
  const [errorMessage, setErrorMessage] = useState("");

  async function save(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPending(true);
    setStatus("idle");
    setErrorMessage("");
    const result = await updateRepositorySettings(
      repository.owner,
      repository.slug,
      { displayName, description, homepageUrl, visibility },
      session.csrfToken,
    );
    setPending(false);
    if (!result.ok) {
      setStatus("error");
      setErrorMessage(mutationFailureMessage(result.kind, dictionary));
      return;
    }
    setStatus("saved");
    router.refresh();
  }

  return (
    <form className={styles.form} onSubmit={save}>
      <label>
        <span>{dictionary.settingsPage.displayName}</span>
        <input maxLength={200} onChange={(event) => setDisplayName(event.target.value)} required value={displayName} />
      </label>
      <label>
        <span>{dictionary.settingsPage.descriptionLabel}</span>
        <textarea
          maxLength={10_000}
          onChange={(event) => setDescription(event.target.value)}
          rows={4}
          value={description}
        />
      </label>
      <label>
        <span>{dictionary.profile.website}</span>
        <input
          maxLength={500}
          onChange={(event) => setHomepageUrl(event.target.value)}
          type="url"
          value={homepageUrl}
        />
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
        {status === "error" && <span role="alert">{errorMessage}</span>}
      </div>
    </form>
  );
}
