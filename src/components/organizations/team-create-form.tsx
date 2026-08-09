"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, Team } from "@/lib/api-types";
import { postJson } from "@/lib/auth-client";

import styles from "./organization-settings-form.module.css";

type TeamCreateFormProps = {
  dictionary: Dictionary;
  locale: "en" | "ja";
  organizationSlug: string;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

export function TeamCreateForm({ dictionary, locale, organizationSlug, session }: TeamCreateFormProps) {
  const router = useRouter();
  const [slug, setSlug] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [description, setDescription] = useState("");
  const [status, setStatus] = useState<"idle" | "error">("idle");
  const [pending, setPending] = useState(false);

  async function createTeam(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPending(true);
    setStatus("idle");
    const result = await postJson<Team>(
      `/api/v1/organizations/${encodeURIComponent(organizationSlug)}/teams`,
      { slug, displayName, description },
      session.csrfToken,
    );
    if (!result.ok) {
      setPending(false);
      setStatus("error");
      return;
    }
    router.push(`/${locale}/organizations/${encodeURIComponent(organizationSlug)}/teams/${result.data.slug}`);
    router.refresh();
  }

  return (
    <form className={styles.form} onSubmit={createTeam}>
      <div className={styles.heading}>
        <h2>{dictionary.organizationPage.newTeam}</h2>
      </div>
      <div className={styles.grid}>
        <label>
          <span>{dictionary.organizationPage.teamSlug}</span>
          <input
            onChange={(event) => setSlug(event.target.value)}
            pattern="[a-z0-9]+(?:-[a-z0-9]+)*"
            required
            value={slug}
          />
        </label>
        <label>
          <span>{dictionary.profile.displayName}</span>
          <input onChange={(event) => setDisplayName(event.target.value)} required value={displayName} />
        </label>
      </div>
      <label>
        <span>{dictionary.forms.description}</span>
        <textarea onChange={(event) => setDescription(event.target.value)} rows={3} value={description} />
      </label>
      <div className={styles.actions}>
        <button disabled={pending} type="submit">
          {pending ? dictionary.common.loading : dictionary.organizationPage.createTeam}
        </button>
        {status === "error" && <span role="alert">{dictionary.forms.submitFailed}</span>}
      </div>
    </form>
  );
}
