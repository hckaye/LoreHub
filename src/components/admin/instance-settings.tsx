"use client";

import { useState } from "react";

import type { Dictionary } from "@/i18n";
import {
  hostedLoreServerChoice,
  hostedLoreServerOverride,
  isHostedLoreServerChoice,
  type AdminSettings,
  type HostedLoreServerChoice,
} from "@/lib/admin-settings";
import { updateAdminSettings } from "@/lib/admin-settings-client";
import type { AuthSession } from "@/lib/api-types";
import { mutationFailureMessage } from "@/lib/mutation-messages";

import styles from "@/components/account/settings-form.module.css";

type InstanceSettingsProps = {
  dictionary: Dictionary;
  initialSettings: AdminSettings;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

export function InstanceSettings(props: InstanceSettingsProps) {
  const copy = props.dictionary.instanceSettings;
  const [settings, setSettings] = useState(props.initialSettings);
  const [choice, setChoice] = useState<HostedLoreServerChoice>(
    hostedLoreServerChoice(props.initialSettings.hostedLoreServerOverride),
  );
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const defaultStatus = settings.hostedLoreServerDefault ? copy.statusEnabled : copy.statusDisabled;

  async function save(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setNotice("");
    setSaving(true);
    const result = await updateAdminSettings(hostedLoreServerOverride(choice), props.session.csrfToken);
    setSaving(false);
    if (!result.ok) {
      setError(result.kind === "invalid" ? copy.saveFailed : mutationFailureMessage(result.kind, props.dictionary));
      return;
    }
    setSettings(result.data);
    setChoice(hostedLoreServerChoice(result.data.hostedLoreServerOverride));
    setNotice(copy.saved);
  }

  return (
    <form className={styles.form} onSubmit={save}>
      {notice ? (
        <div className={styles.flash} role="status">
          {notice}
        </div>
      ) : null}
      {error ? (
        <div className={styles.flash} data-tone="error" role="alert">
          {error}
        </div>
      ) : null}
      <h2 className={styles.subhead}>{copy.hostedLoreServer}</h2>
      <div aria-label={copy.hostedLoreServer} className={styles.box} role="radiogroup">
        <RadioRow
          checked={choice === "default"}
          help={copy.followDefaultHelp}
          label={copy.followDefault.replace("{status}", defaultStatus)}
          onChange={() => setChoice("default")}
          value="default"
        />
        <RadioRow
          checked={choice === "enabled"}
          help={copy.enabledHelp}
          label={copy.enabled}
          onChange={() => setChoice("enabled")}
          value="enabled"
        />
        <RadioRow
          checked={choice === "disabled"}
          help={copy.disabledHelp}
          label={copy.disabled}
          onChange={() => setChoice("disabled")}
          value="disabled"
        />
      </div>
      <p className={styles.help}>{copy.explanation}</p>
      <button className={styles.primaryButton} disabled={saving} type="submit">
        {saving ? copy.saving : copy.save}
      </button>
    </form>
  );
}

function RadioRow({
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
  value: HostedLoreServerChoice;
}) {
  return (
    <label className={styles.row}>
      <input
        checked={checked}
        name="hosted-lore-server"
        onChange={(event) => {
          if (isHostedLoreServerChoice(event.target.value)) onChange();
        }}
        type="radio"
        value={value}
      />
      <span className={styles.rowBody}>
        <strong>{label}</strong>
        <p className={styles.rowHint}>{help}</p>
      </span>
    </label>
  );
}
