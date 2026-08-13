"use client";

import { ShieldCheck } from "lucide-react";
import { useState } from "react";

import { EmptyState } from "@/components/ui/empty-state";
import { StatusBadge } from "@/components/ui/status-badge";
import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession } from "@/lib/api-types";
import { grantEntitlement, revokeEntitlement } from "@/lib/entitlement-client";
import {
  entitlementFeatures,
  entitlementKey,
  isEntitlementFeature,
  isEntitlementSubjectID,
  type Entitlement,
  type EntitlementFeature,
  type EntitlementSubject,
} from "@/lib/entitlements";
import { formatDate } from "@/lib/format";
import { mutationFailureMessage } from "@/lib/mutation-messages";

import styles from "@/components/settings/settings-panel.module.css";

type EntitlementSettingsProps = {
  dictionary: Dictionary;
  initialEntitlements: Entitlement[];
  locale: Locale;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

export function EntitlementSettings(props: EntitlementSettingsProps) {
  const copy = props.dictionary.entitlementSettings;
  const [entitlements, setEntitlements] = useState(props.initialEntitlements);
  const [subjectKind, setSubjectKind] = useState<EntitlementSubject["kind"]>("organization");
  const [subjectID, setSubjectID] = useState("");
  const [feature, setFeature] = useState<EntitlementFeature>("hosted_runners");
  const [granting, setGranting] = useState(false);
  const [revoking, setRevoking] = useState("");
  const [confirming, setConfirming] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  async function grant(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setNotice("");
    if (!isEntitlementSubjectID(subjectID)) {
      setError(copy.invalidSubject);
      return;
    }
    setGranting(true);
    const result = await grantEntitlement({ kind: subjectKind, id: subjectID }, feature, props.session.csrfToken);
    setGranting(false);
    if (!result.ok) {
      setError(result.kind === "invalid" ? copy.grantFailed : mutationFailureMessage(result.kind, props.dictionary));
      return;
    }
    const granted = result.data;
    setEntitlements((current) => [
      granted,
      ...current.filter((item) => entitlementKey(item) !== entitlementKey(granted)),
    ]);
    setSubjectID("");
    setNotice(copy.granted);
  }

  async function revoke(entitlement: Entitlement) {
    const subject: EntitlementSubject = entitlement.organizationId
      ? { kind: "organization", id: entitlement.organizationId }
      : { kind: "user", id: entitlement.userId ?? "" };
    if (!isEntitlementFeature(entitlement.feature)) return;
    setRevoking(entitlementKey(entitlement));
    setError("");
    setNotice("");
    const result = await revokeEntitlement(subject, entitlement.feature, props.session.csrfToken);
    setRevoking("");
    setConfirming("");
    if (!result.ok) {
      setError(result.kind === "invalid" ? copy.revokeFailed : mutationFailureMessage(result.kind, props.dictionary));
      return;
    }
    const revokedAt = new Date().toISOString();
    setEntitlements((current) =>
      current.map((item) => (entitlementKey(item) === entitlementKey(entitlement) ? { ...item, revokedAt } : item)),
    );
    setNotice(copy.revoked);
  }

  return (
    <div className={styles.settings}>
      <form className={styles.form} onSubmit={grant}>
        <h3>{copy.grantTitle}</h3>
        <div className={styles.formGrid}>
          <label>
            {copy.subject}
            <select
              onChange={(event) => setSubjectKind(event.target.value === "user" ? "user" : "organization")}
              value={subjectKind}
            >
              <option value="organization">{copy.subjectKinds.organization}</option>
              <option value="user">{copy.subjectKinds.user}</option>
            </select>
          </label>
          <label>
            {subjectKind === "organization" ? copy.organizationId : copy.userId}
            <input
              onChange={(event) => setSubjectID(event.target.value)}
              placeholder={copy.subjectPlaceholder}
              value={subjectID}
            />
          </label>
          <label>
            {copy.feature}
            <select
              onChange={(event) => {
                if (isEntitlementFeature(event.target.value)) setFeature(event.target.value);
              }}
              value={feature}
            >
              {entitlementFeatures.map((value) => (
                <option key={value} value={value}>
                  {copy.features[value]}
                </option>
              ))}
            </select>
          </label>
        </div>
        <p className={styles.hint}>{copy.subjectHint}</p>
        <div className={styles.formActions}>
          <button className={styles.primaryButton} disabled={granting} type="submit">
            {granting ? copy.granting : copy.grant}
          </button>
          {error && (
            <span className={styles.error} role="alert">
              {error}
            </span>
          )}
          {notice && (
            <span className={styles.notice} role="status">
              {notice}
            </span>
          )}
        </div>
      </form>
      <h3>{copy.listTitle}</h3>
      {entitlements.length === 0 ? (
        <EmptyState body={copy.emptyBody} icon={<ShieldCheck aria-hidden="true" />} title={copy.empty} />
      ) : (
        <ul className={styles.list}>
          {entitlements.map((entitlement) => (
            <EntitlementRow
              confirming={confirming === entitlementKey(entitlement)}
              copy={copy}
              entitlement={entitlement}
              key={entitlementKey(entitlement)}
              locale={props.locale}
              onCancel={() => setConfirming("")}
              onConfirm={() => setConfirming(entitlementKey(entitlement))}
              onRevoke={() => void revoke(entitlement)}
              revoking={revoking === entitlementKey(entitlement)}
            />
          ))}
        </ul>
      )}
    </div>
  );
}

function EntitlementRow({
  confirming,
  copy,
  entitlement,
  locale,
  onCancel,
  onConfirm,
  onRevoke,
  revoking,
}: {
  confirming: boolean;
  copy: Dictionary["entitlementSettings"];
  entitlement: Entitlement;
  locale: Locale;
  onCancel: () => void;
  onConfirm: () => void;
  onRevoke: () => void;
  revoking: boolean;
}) {
  const feature = isEntitlementFeature(entitlement.feature) ? copy.features[entitlement.feature] : entitlement.feature;
  const source = entitlement.grantSource === "billing" ? copy.sources.billing : copy.sources.manual;
  return (
    <li>
      <div className={styles.rowDetails}>
        <div className={styles.rowHeading}>
          <strong>{feature}</strong>
          {entitlement.revokedAt && <StatusBadge tone="danger">{copy.revokedBadge}</StatusBadge>}
        </div>
        <div className={styles.rowMeta}>
          <span>
            {entitlement.organizationId ? copy.subjectKinds.organization : copy.subjectKinds.user}{" "}
            <code>{entitlement.organizationId ?? entitlement.userId}</code>
          </span>
          <span>
            {copy.source} {source}
          </span>
          <span>
            {copy.grantedAt} {formatDate(entitlement.createdAt, locale)}
          </span>
        </div>
      </div>
      <div className={styles.rowActions}>
        {entitlement.revokedAt ? null : confirming ? (
          <span className={styles.confirm}>
            {copy.revokeConfirm}
            <button className={styles.dangerButton} disabled={revoking} onClick={onRevoke} type="button">
              {revoking ? copy.revoking : copy.revokeConfirmAction}
            </button>
            <button className={styles.secondaryButton} disabled={revoking} onClick={onCancel} type="button">
              {copy.cancel}
            </button>
          </span>
        ) : (
          <button className={styles.dangerButton} onClick={onConfirm} type="button">
            {copy.revoke}
          </button>
        )}
      </div>
    </li>
  );
}
