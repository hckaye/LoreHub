"use client";

import { useState } from "react";

import { UserAvatar } from "@/components/ui/user-avatar";
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
  const copy = dictionary.accountSettings;
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
    <>
      {status === "saved" && (
        <div className={styles.flash} role="status">
          {copy.profileUpdated}
        </div>
      )}
      {status === "error" && (
        <div className={styles.flash} data-tone="error" role="alert">
          {dictionary.forms.submitFailed}
        </div>
      )}
      <div className={styles.profileColumns}>
        <form className={styles.form} onSubmit={save}>
          <label className={styles.field}>
            <span>{copy.name}</span>
            <input onChange={(event) => setDisplayName(event.target.value)} value={displayName} />
            <p className={styles.help}>{copy.nameHelp}</p>
          </label>
          <label className={styles.field}>
            <span>{dictionary.profile.bio}</span>
            <textarea onChange={(event) => setBio(event.target.value)} rows={4} value={bio} />
          </label>
          <label className={styles.field}>
            <span>{dictionary.profile.pronouns}</span>
            <input onChange={(event) => setPronouns(event.target.value)} value={pronouns} />
          </label>
          <label className={styles.field}>
            <span>{copy.url}</span>
            <input onChange={(event) => setWebsiteUrl(event.target.value)} value={websiteUrl} />
          </label>
          <label className={styles.field}>
            <span>{dictionary.profile.company}</span>
            <input onChange={(event) => setCompany(event.target.value)} value={company} />
          </label>
          <label className={styles.field}>
            <span>{dictionary.profile.location}</span>
            <input onChange={(event) => setLocation(event.target.value)} value={location} />
          </label>
          <button className={styles.primaryButton} disabled={pending} type="submit">
            {pending ? dictionary.common.loading : copy.updateProfile}
          </button>
        </form>
        <aside className={styles.picture}>
          <h2 className={styles.pictureLabel}>{copy.profilePicture}</h2>
          <UserAvatar avatarUrl={profile.avatarUrl} name={displayName || profile.username} size={200} />
        </aside>
      </div>
    </>
  );
}
