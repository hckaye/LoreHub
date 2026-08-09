"use client";

import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, UserProfile } from "@/lib/api-types";
import { patchJson } from "@/lib/auth-client";

import styles from "./settings-form.module.css";

type ProfileSettingsFormProps = {
  dictionary: Dictionary;
  profile: UserProfile;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

export function ProfileSettingsForm({ dictionary, profile, session }: ProfileSettingsFormProps) {
  const [displayName, setDisplayName] = useState(profile.displayName);
  const [bio, setBio] = useState(profile.bio);
  const [websiteUrl, setWebsiteUrl] = useState(profile.websiteUrl);
  const [location, setLocation] = useState(profile.location);
  const [company, setCompany] = useState(profile.company);
  const [pronouns, setPronouns] = useState(profile.pronouns);
  const [status, setStatus] = useState<"idle" | "saved" | "error">("idle");
  const [pending, setPending] = useState(false);

  async function save(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPending(true);
    setStatus("idle");
    const result = await patchJson<UserProfile>(
      "/api/v1/account/profile",
      { displayName, bio, websiteUrl, location, company, pronouns },
      session.csrfToken,
    );
    setPending(false);
    setStatus(result.ok ? "saved" : "error");
  }

  return (
    <form className={styles.form} onSubmit={save}>
      <div className={styles.grid}>
        <label>
          <span>{dictionary.profile.displayName}</span>
          <input onChange={(event) => setDisplayName(event.target.value)} required value={displayName} />
        </label>
        <label>
          <span>{dictionary.profile.pronouns}</span>
          <input onChange={(event) => setPronouns(event.target.value)} value={pronouns} />
        </label>
        <label>
          <span>{dictionary.profile.company}</span>
          <input onChange={(event) => setCompany(event.target.value)} value={company} />
        </label>
        <label>
          <span>{dictionary.profile.location}</span>
          <input onChange={(event) => setLocation(event.target.value)} value={location} />
        </label>
      </div>
      <label>
        <span>{dictionary.profile.website}</span>
        <input onChange={(event) => setWebsiteUrl(event.target.value)} value={websiteUrl} />
      </label>
      <label>
        <span>{dictionary.profile.bio}</span>
        <textarea onChange={(event) => setBio(event.target.value)} rows={4} value={bio} />
      </label>
      <div className={styles.actions}>
        <button disabled={pending} type="submit">
          {pending ? dictionary.common.loading : dictionary.profile.saveProfile}
        </button>
        {status === "saved" && <span role="status">{dictionary.profile.saved}</span>}
        {status === "error" && <span role="alert">{dictionary.forms.submitFailed}</span>}
      </div>
    </form>
  );
}
