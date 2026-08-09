"use client";

import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, NotificationPreferences } from "@/lib/api-types";
import { patchJson } from "@/lib/auth-client";

import styles from "./settings-form.module.css";

type NotificationSettingsFormProps = {
  dictionary: Dictionary;
  preferences: NotificationPreferences;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

export function NotificationSettingsForm({ dictionary, preferences, session }: NotificationSettingsFormProps) {
  const [values, setValues] = useState(preferences);
  const [status, setStatus] = useState<"idle" | "saved" | "error">("idle");
  const [pending, setPending] = useState(false);

  function setValue(key: keyof NotificationPreferences, value: boolean) {
    setValues((current) => ({ ...current, [key]: value }));
  }

  async function save(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPending(true);
    setStatus("idle");
    const result = await patchJson<NotificationPreferences>(
      "/api/v1/account/notification-preferences",
      {
        inAppEnabled: values.inAppEnabled,
        emailEnabled: values.emailEnabled,
        mentionEnabled: values.mentionEnabled,
        teamEnabled: values.teamEnabled,
        repositoryEnabled: values.repositoryEnabled,
      },
      session.csrfToken,
    );
    setPending(false);
    setStatus(result.ok ? "saved" : "error");
  }

  return (
    <form className={styles.form} onSubmit={save}>
      <label className={styles.checkbox}>
        <input
          checked={values.inAppEnabled}
          onChange={(event) => setValue("inAppEnabled", event.target.checked)}
          type="checkbox"
        />
        <span>{dictionary.accountSettings.inApp}</span>
      </label>
      <label className={styles.checkbox}>
        <input
          checked={values.emailEnabled}
          onChange={(event) => setValue("emailEnabled", event.target.checked)}
          type="checkbox"
        />
        <span>{dictionary.accountSettings.email}</span>
      </label>
      <label className={styles.checkbox}>
        <input
          checked={values.mentionEnabled}
          onChange={(event) => setValue("mentionEnabled", event.target.checked)}
          type="checkbox"
        />
        <span>{dictionary.accountSettings.mentions}</span>
      </label>
      <label className={styles.checkbox}>
        <input
          checked={values.teamEnabled}
          onChange={(event) => setValue("teamEnabled", event.target.checked)}
          type="checkbox"
        />
        <span>{dictionary.accountSettings.teamEvents}</span>
      </label>
      <label className={styles.checkbox}>
        <input
          checked={values.repositoryEnabled}
          onChange={(event) => setValue("repositoryEnabled", event.target.checked)}
          type="checkbox"
        />
        <span>{dictionary.accountSettings.repositoryEvents}</span>
      </label>
      <div className={styles.actions}>
        <button disabled={pending} type="submit">
          {pending ? dictionary.common.loading : dictionary.accountSettings.savePreferences}
        </button>
        {status === "saved" && <span role="status">{dictionary.accountSettings.preferencesSaved}</span>}
        {status === "error" && <span role="alert">{dictionary.forms.submitFailed}</span>}
      </div>
    </form>
  );
}
