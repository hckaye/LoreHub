"use client";

import { Copy, KeyRound, Trash2 } from "lucide-react";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, PersonalAccessToken, PersonalAccessTokenScope } from "@/lib/api-types";
import { formatDateTime } from "@/lib/format";
import { createPersonalAccessToken, revokePersonalAccessToken } from "@/lib/personal-access-token-client";

import styles from "./personal-access-token-settings.module.css";

type PersonalAccessTokenSettingsProps = {
  dictionary: Dictionary;
  initialTokens: PersonalAccessToken[];
  locale: "en" | "ja";
  session: Extract<AuthSession, { status: "authenticated" }>;
};

const scopeOptions: { scope: PersonalAccessTokenScope; label: keyof Dictionary["personalAccessTokens"]["scopes"] }[] = [
  { scope: "read_api", label: "readApi" },
  { scope: "api", label: "api" },
  { scope: "read_repository", label: "readRepository" },
  { scope: "write_repository", label: "writeRepository" },
];

export function PersonalAccessTokenSettings(props: PersonalAccessTokenSettingsProps) {
  const copy = props.dictionary.personalAccessTokens;
  const [tokens, setTokens] = useState(props.initialTokens);
  const [name, setName] = useState("");
  const [days, setDays] = useState("30");
  const [scopes, setScopes] = useState<PersonalAccessTokenScope[]>(["read_repository"]);
  const [createdValue, setCreatedValue] = useState("");
  const [pending, setPending] = useState(false);
  const [revoking, setRevoking] = useState<string | null>(null);
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
    const expiresAt = new Date(Date.now() + Number(days) * 24 * 60 * 60 * 1_000).toISOString();
    const result = await createPersonalAccessToken({ name: name.trim(), scopes, expiresAt }, props.session.csrfToken);
    setPending(false);
    if (!result.ok) {
      setError(copy.createFailed);
      return;
    }
    setTokens((current) => [result.data.token, ...current]);
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

  async function revoke(token: PersonalAccessToken) {
    if (!window.confirm(copy.revokeConfirm)) return;
    setRevoking(token.id);
    setError(null);
    const result = await revokePersonalAccessToken(token.id, props.session.csrfToken);
    setRevoking(null);
    if (!result.ok) {
      setError(copy.revokeFailed);
      return;
    }
    setTokens((current) =>
      current.map((item) => (item.id === token.id ? { ...item, revokedAt: new Date().toISOString() } : item)),
    );
  }

  return (
    <div className={styles.settings}>
      {createdValue && (
        <section aria-labelledby="new-personal-access-token" className={styles.created}>
          <div className={styles.createdHeading}>
            <KeyRound aria-hidden="true" size={18} />
            <div>
              <h3 id="new-personal-access-token">{copy.createdTitle}</h3>
              <p>{copy.createdBody}</p>
            </div>
          </div>
          <div className={styles.secretRow}>
            <input aria-label={copy.createdTitle} readOnly value={createdValue} />
            <button onClick={copyToken} type="button">
              <Copy aria-hidden="true" size={16} />
              {copied ? copy.copied : copy.copy}
            </button>
            <button
              className={styles.secondaryButton}
              onClick={() => {
                setCreatedValue("");
                setCopied(false);
              }}
              type="button"
            >
              {copy.dismiss}
            </button>
          </div>
        </section>
      )}

      <form className={styles.form} onSubmit={create}>
        <h3>{copy.createTitle}</h3>
        <div className={styles.formGrid}>
          <label>
            <span>{copy.name}</span>
            <input
              maxLength={80}
              onChange={(event) => setName(event.target.value)}
              placeholder={copy.namePlaceholder}
              required
              value={name}
            />
          </label>
          <label>
            <span>{copy.expiration}</span>
            <select onChange={(event) => setDays(event.target.value)} value={days}>
              <option value="7">{copy.expiresIn.sevenDays}</option>
              <option value="30">{copy.expiresIn.thirtyDays}</option>
              <option value="90">{copy.expiresIn.ninetyDays}</option>
              <option value="365">{copy.expiresIn.oneYear}</option>
            </select>
          </label>
        </div>
        <fieldset>
          <legend>{copy.permissions}</legend>
          <div className={styles.scopeGrid}>
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
        <div className={styles.actions}>
          <button disabled={pending || name.trim() === ""} type="submit">
            {pending ? copy.creating : copy.create}
          </button>
          {error && <span role="alert">{error}</span>}
        </div>
      </form>

      <section aria-labelledby="personal-access-token-list" className={styles.listSection}>
        <h3 id="personal-access-token-list">{copy.existingTitle}</h3>
        {tokens.length === 0 ? (
          <p className={styles.empty}>{copy.empty}</p>
        ) : (
          <ul className={styles.tokenList}>
            {tokens.map((token) => {
              const status = tokenStatus(token);
              return (
                <li key={token.id}>
                  <div className={styles.tokenDetails}>
                    <div className={styles.tokenHeading}>
                      <strong>{token.name}</strong>
                      <span data-status={status}>{copy[status]}</span>
                    </div>
                    <code>
                      {copy.prefix}: {token.prefix}…
                    </code>
                    <div className={styles.scopeList}>
                      {token.scopes.map((scope) => (
                        <code key={scope}>{scope}</code>
                      ))}
                    </div>
                    <small>
                      {token.lastUsedAt
                        ? copy.lastUsed.replace("{date}", formatDateTime(token.lastUsedAt, props.locale))
                        : copy.neverUsed}
                      {" · "}
                      {copy.expires.replace("{date}", formatDateTime(token.expiresAt, props.locale))}
                    </small>
                  </div>
                  {status === "active" && (
                    <button
                      className={styles.revokeButton}
                      disabled={revoking === token.id}
                      onClick={() => revoke(token)}
                      type="button"
                    >
                      <Trash2 aria-hidden="true" size={15} />
                      {copy.revoke}
                    </button>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </section>
    </div>
  );
}

function tokenStatus(token: PersonalAccessToken): "active" | "expired" | "revoked" {
  if (token.revokedAt) return "revoked";
  return Date.parse(token.expiresAt) <= Date.now() ? "expired" : "active";
}
