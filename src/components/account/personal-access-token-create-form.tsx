"use client";

import Link from "next/link";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, PersonalAccessTokenScope } from "@/lib/api-types";
import { createPersonalAccessToken } from "@/lib/personal-access-token-client";
import { accountSettingsPath } from "@/lib/routes";

import styles from "./personal-access-token-settings.module.css";

type PersonalAccessTokenCreateFormProps = {
  dictionary: Dictionary;
  locale: Locale;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

const scopeOptions: {
  scope: PersonalAccessTokenScope;
  label: keyof Dictionary["personalAccessTokens"]["scopes"];
}[] = [
  { scope: "read_api", label: "readApi" },
  { scope: "api", label: "api" },
  { scope: "read_repository", label: "readRepository" },
  { scope: "write_repository", label: "writeRepository" },
];

export function PersonalAccessTokenCreateForm(props: PersonalAccessTokenCreateFormProps) {
  const copy = props.dictionary.personalAccessTokens;
  const [name, setName] = useState("");
  const [days, setDays] = useState("30");
  const [scopes, setScopes] = useState<PersonalAccessTokenScope[]>(["read_repository"]);
  const [createdValue, setCreatedValue] = useState("");
  const [pending, setPending] = useState(false);
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState<string | null>(null);

  function toggleScope(scope: PersonalAccessTokenScope, selected: boolean) {
    setScopes((current) => (selected ? [...current, scope] : current.filter((value) => value !== scope)));
  }

  async function create(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (scopes.length === 0) {
      setError(copy.choosePermission);
      return;
    }
    setPending(true);
    setError(null);
    setCopied(false);
    const result = await createPersonalAccessToken(
      { name: name.trim(), scopes, expiresAt: expirationDate(days).toISOString() },
      props.session.csrfToken,
    );
    setPending(false);
    if (!result.ok) {
      setError(copy.createFailed);
      return;
    }
    setCreatedValue(result.data.value);
    setName("");
  }

  async function copyToken() {
    try {
      await navigator.clipboard.writeText(createdValue);
      setCopied(true);
      setError(null);
    } catch {
      setError(copy.copyFailed);
    }
  }

  return (
    <div>
      {createdValue && (
        <section aria-labelledby="new-personal-access-token" className={styles.flash}>
          <h2 id="new-personal-access-token">{copy.createdTitle}</h2>
          <p>{copy.createdBody}</p>
          <div className={styles.secretRow}>
            <input aria-label={copy.createdTitle} readOnly value={createdValue} />
            <button onClick={() => void copyToken()} type="button">
              {copied ? copy.copied : copy.copy}
            </button>
          </div>
          <Link className={styles.backLink} href={accountSettingsPath(props.locale, "tokens")}>
            {copy.backToTokens}
          </Link>
        </section>
      )}

      <form className={styles.form} onSubmit={create}>
        <label className={styles.field}>
          <span>{copy.name}</span>
          <input
            maxLength={80}
            onChange={(event) => setName(event.target.value)}
            placeholder={copy.namePlaceholder}
            required
            value={name}
          />
        </label>
        <label className={styles.field}>
          <span>{copy.expiration}</span>
          <select onChange={(event) => setDays(event.target.value)} value={days}>
            <option value="7">{copy.expiresIn.sevenDays}</option>
            <option value="30">{copy.expiresIn.thirtyDays}</option>
            <option value="90">{copy.expiresIn.ninetyDays}</option>
            <option value="365">{copy.expiresIn.oneYear}</option>
          </select>
          <p>{copy.expiresOn.replace("{date}", expirationLabel(days, copy.expiresIn))}</p>
        </label>
        <fieldset className={styles.field}>
          <legend>{copy.selectScopes}</legend>
          <div className={styles.scopeList}>
            {scopeOptions.map((option) => (
              <label className={styles.scope} key={option.scope}>
                <input
                  checked={scopes.includes(option.scope)}
                  onChange={(event) => toggleScope(option.scope, event.target.checked)}
                  type="checkbox"
                />
                <span>
                  <code>{option.scope}</code>
                  <small>{copy.scopes[option.label]}</small>
                </span>
              </label>
            ))}
          </div>
        </fieldset>
        <div className={styles.formActions}>
          <button className={styles.primaryButton} disabled={pending || name.trim() === ""} type="submit">
            {pending ? copy.creating : copy.create}
          </button>
          <Link className={styles.cancel} href={accountSettingsPath(props.locale, "tokens")}>
            {props.dictionary.common.cancel}
          </Link>
        </div>
        {error && (
          <p className={styles.error} role="alert">
            {error}
          </p>
        )}
      </form>
    </div>
  );
}

function expirationDate(days: string): Date {
  return new Date(Date.now() + Number(days) * 24 * 60 * 60 * 1_000);
}

function expirationLabel(days: string, labels: Dictionary["personalAccessTokens"]["expiresIn"]): string {
  if (days === "7") return labels.sevenDays;
  if (days === "90") return labels.ninetyDays;
  if (days === "365") return labels.oneYear;
  return labels.thirtyDays;
}
