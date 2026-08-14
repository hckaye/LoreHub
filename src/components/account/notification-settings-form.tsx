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
  const copy = dictionary.accountSettings;
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
      {status === "saved" && (
        <div className={styles.flash} role="status">
          {copy.preferencesSaved}
        </div>
      )}
      {status === "error" && (
        <div className={styles.flash} data-tone="error" role="alert">
          {dictionary.forms.submitFailed}
        </div>
      )}
      <h2 className={styles.subhead}>{copy.subscriptions}</h2>
      <div className={styles.box}>
        <CheckboxRow
          checked={values.inAppEnabled}
          help={copy.inAppHelp}
          label={copy.inApp}
          onChange={(checked) => setValue("inAppEnabled", checked)}
        />
        <CheckboxRow
          checked={values.emailEnabled}
          disabled={!values.emailAvailable}
          help={values.emailAvailable ? copy.emailHelp : dictionary.accountSettings.emailUnavailable}
          label={copy.email}
          onChange={(checked) => setValue("emailEnabled", checked)}
        />
        <CheckboxRow
          checked={values.mentionEnabled}
          help={copy.mentionsHelp}
          label={copy.mentions}
          onChange={(checked) => setValue("mentionEnabled", checked)}
        />
        <CheckboxRow
          checked={values.teamEnabled}
          help={copy.teamEventsHelp}
          label={copy.teamEvents}
          onChange={(checked) => setValue("teamEnabled", checked)}
        />
        <CheckboxRow
          checked={values.repositoryEnabled}
          help={copy.repositoryEventsHelp}
          label={copy.repositoryEvents}
          onChange={(checked) => setValue("repositoryEnabled", checked)}
        />
      </div>
      <button className={styles.primaryButton} disabled={pending} type="submit">
        {pending ? dictionary.common.loading : copy.savePreferences}
      </button>
    </form>
  );
}

function CheckboxRow({
  checked,
  disabled,
  help,
  label,
  onChange,
}: {
  checked: boolean;
  disabled?: boolean;
  help: string;
  label: string;
  onChange: (checked: boolean) => void;
}) {
  return (
    <label className={styles.row}>
      <input
        checked={checked}
        disabled={disabled}
        onChange={(event) => onChange(event.target.checked)}
        type="checkbox"
      />
      <span className={styles.rowBody}>
        <strong>{label}</strong>
        <p className={styles.rowHint}>{help}</p>
      </span>
    </label>
  );
}
