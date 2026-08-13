"use client";

import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession } from "@/lib/api-types";
import type { MutationFailureKind } from "@/lib/auth-client";
import { setDefaultLoreServer } from "@/lib/lore-server-client";
import { loreServerStatus, type LoreServer } from "@/lib/lore-servers";
import { mutationFailureMessage } from "@/lib/mutation-messages";

import styles from "@/components/settings/settings-panel.module.css";

type DefaultLoreServerFormProps = {
  dictionary: Dictionary;
  initialServerID: string;
  organization: string;
  servers: LoreServer[];
  session: Extract<AuthSession, { status: "authenticated" }>;
};

export function DefaultLoreServerForm(props: DefaultLoreServerFormProps) {
  const copy = props.dictionary.loreServerSettings;
  const [serverID, setServerID] = useState(props.initialServerID);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const selectable = props.servers.filter((server) => loreServerStatus(server) !== "revoked");

  async function save(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSaving(true);
    setError("");
    setNotice("");
    const result = await setDefaultLoreServer(props.organization, serverID || null, props.session.csrfToken);
    setSaving(false);
    if (!result.ok) {
      setError(defaultFailureMessage(result.kind, result.code, props.dictionary));
      return;
    }
    setServerID(result.data?.id ?? "");
    setNotice(copy.defaultSaved);
  }

  return (
    <form className={styles.form} onSubmit={save}>
      <div>
        <h3>{copy.defaultTitle}</h3>
        <p className={styles.hint}>{copy.defaultDescription}</p>
      </div>
      <div className={styles.selectRow}>
        <label>
          {copy.defaultLabel}
          <select onChange={(event) => setServerID(event.target.value)} value={serverID}>
            <option value="">{copy.defaultNone}</option>
            {selectable.map((server) => (
              <option key={server.id} value={server.id}>
                {server.name} ({server.publicUrl})
              </option>
            ))}
          </select>
        </label>
        <button className={styles.secondaryButton} disabled={saving} type="submit">
          {saving ? copy.saving : copy.save}
        </button>
      </div>
      {error && (
        <p className={styles.error} role="alert">
          {error}
        </p>
      )}
      {notice && (
        <p className={styles.notice} role="status">
          {notice}
        </p>
      )}
    </form>
  );
}

function defaultFailureMessage(kind: MutationFailureKind, code: string | null, dictionary: Dictionary): string {
  const copy = dictionary.loreServerSettings;
  if (code === "hosted_lore_server_entitlement_required" || code === "no_lore_server_available") {
    return copy.entitlementRequired;
  }
  if (kind === "invalid" || kind === "conflict") {
    return copy.defaultFailed;
  }
  return mutationFailureMessage(kind, dictionary);
}
