"use client";

import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, PersonalAccessToken } from "@/lib/api-types";
import { formatDateTime } from "@/lib/format";
import { revokePersonalAccessToken } from "@/lib/personal-access-token-client";

import styles from "./personal-access-token-settings.module.css";

type PersonalAccessTokenListProps = {
  dictionary: Dictionary;
  initialTokens: PersonalAccessToken[];
  locale: "en" | "ja";
  session: Extract<AuthSession, { status: "authenticated" }>;
};

export function PersonalAccessTokenList(props: PersonalAccessTokenListProps) {
  const copy = props.dictionary.personalAccessTokens;
  const [tokens, setTokens] = useState(props.initialTokens);
  const [revoking, setRevoking] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

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

  if (tokens.length === 0) {
    return <p className={styles.empty}>{copy.empty}</p>;
  }

  return (
    <div>
      {error && (
        <p className={styles.error} role="alert">
          {error}
        </p>
      )}
      <ul className={styles.tokenList}>
        {tokens.map((token) => {
          const status = tokenStatus(token);
          return (
            <li key={token.id}>
              <div className={styles.tokenDetails}>
                <div className={styles.tokenHeading}>
                  <strong>{token.name}</strong>
                  {status !== "active" && <span data-status={status}>{copy[status]}</span>}
                </div>
                <span className={styles.tokenMeta}>
                  {token.lastUsedAt
                    ? copy.lastUsed.replace("{date}", formatDateTime(token.lastUsedAt, props.locale))
                    : copy.neverUsed}
                  {" — "}
                  {copy.expires.replace("{date}", formatDateTime(token.expiresAt, props.locale))}
                </span>
              </div>
              {status === "active" && (
                <button
                  className={styles.deleteButton}
                  disabled={revoking === token.id}
                  onClick={() => void revoke(token)}
                  type="button"
                >
                  {copy.revoke}
                </button>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}

function tokenStatus(token: PersonalAccessToken): "active" | "expired" | "revoked" {
  if (token.revokedAt) return "revoked";
  return Date.parse(token.expiresAt) <= Date.now() ? "expired" : "active";
}
