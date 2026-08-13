"use client";

import { Server, TriangleAlert } from "lucide-react";
import { useState } from "react";

import { TokenRegistrationPanel } from "@/components/settings/token-registration-panel";
import { EmptyState } from "@/components/ui/empty-state";
import { StatusBadge } from "@/components/ui/status-badge";
import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession } from "@/lib/api-types";
import { formatDate, formatExpiryNote, formatRelativeTime } from "@/lib/format";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { createRunnerRegistrationToken, revokeRunner } from "@/lib/runner-client";
import { runnerConfigureCommand, runnerRunCommand, runnerStatus, type Runner, type RunnerTarget } from "@/lib/runners";

import styles from "@/components/settings/settings-panel.module.css";

type RunnerSettingsProps = {
  dictionary: Dictionary;
  initialRunners: Runner[];
  locale: Locale;
  session: Extract<AuthSession, { status: "authenticated" }>;
  target: RunnerTarget;
};

type Registration = { token: string; expiresAt: string };

const statusTone = {
  idle: "success",
  offline: "neutral",
  expired: "warning",
  revoked: "danger",
} as const;

export function RunnerSettings(props: RunnerSettingsProps) {
  const copy = props.dictionary.runnerSettings;
  const [runners, setRunners] = useState(props.initialRunners);
  const [registration, setRegistration] = useState<Registration | null>(null);
  const [origin, setOrigin] = useState("https://lorehub.example");
  const [creating, setCreating] = useState(false);
  const [revoking, setRevoking] = useState("");
  const [confirming, setConfirming] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  async function createToken() {
    setCreating(true);
    setError("");
    setNotice("");
    const result = await createRunnerRegistrationToken(props.target, props.session.csrfToken);
    setCreating(false);
    if (!result.ok) {
      setError(result.kind === "invalid" ? copy.createFailed : mutationFailureMessage(result.kind, props.dictionary));
      return;
    }
    setOrigin(window.location.origin);
    setRegistration(result.data);
  }

  async function remove(runner: Runner) {
    setRevoking(runner.id);
    setError("");
    setNotice("");
    const result = await revokeRunner(props.target, runner.id, props.session.csrfToken);
    setRevoking("");
    setConfirming("");
    if (!result.ok) {
      setError(result.kind === "invalid" ? copy.revokeFailed : mutationFailureMessage(result.kind, props.dictionary));
      return;
    }
    setRunners((current) =>
      current.map((item) => (item.id === runner.id ? { ...item, revokedAt: new Date().toISOString() } : item)),
    );
    setNotice(copy.revoked);
  }

  return (
    <div className={styles.settings}>
      {props.target.kind === "organization" && (
        <div className={styles.warning} role="note">
          <TriangleAlert aria-hidden="true" size={18} />
          <div>
            <strong>{copy.organizationWarningTitle}</strong>
            <p>{copy.organizationWarningBody}</p>
          </div>
        </div>
      )}
      <div className={styles.toolbar}>
        <p>{props.target.kind === "organization" ? copy.organizationDescription : copy.repositoryDescription}</p>
        <button className={styles.primaryButton} disabled={creating} onClick={() => void createToken()} type="button">
          {creating ? copy.creating : copy.newRunner}
        </button>
      </div>
      {registration && (
        <TokenRegistrationPanel
          body={copy.registrationBody}
          commands={[runnerConfigureCommand(origin), runnerRunCommand()]}
          copiedLabel={copy.copied}
          copyCommandLabel={copy.copyCommand}
          copyTokenLabel={copy.copyToken}
          dismissLabel={copy.dismiss}
          expiryNote={formatExpiryNote(copy.tokenExpires, registration.expiresAt, props.locale)}
          hints={[copy.tokenOnce, copy.stdinNote, copy.downloadNote]}
          onDismiss={() => setRegistration(null)}
          title={copy.registrationTitle}
          token={registration.token}
          tokenLabel={copy.tokenLabel}
        />
      )}
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
      {runners.length === 0 ? (
        <EmptyState body={copy.emptyBody} icon={<Server aria-hidden="true" />} title={copy.empty} />
      ) : (
        <ul className={styles.list}>
          {runners.map((runner) => (
            <RunnerRow
              confirming={confirming === runner.id}
              copy={copy}
              key={runner.id}
              locale={props.locale}
              onCancel={() => setConfirming("")}
              onConfirm={() => setConfirming(runner.id)}
              onRevoke={() => void remove(runner)}
              revoking={revoking === runner.id}
              runner={runner}
            />
          ))}
        </ul>
      )}
    </div>
  );
}

function RunnerRow({
  confirming,
  copy,
  locale,
  onCancel,
  onConfirm,
  onRevoke,
  revoking,
  runner,
}: {
  confirming: boolean;
  copy: Dictionary["runnerSettings"];
  locale: Locale;
  onCancel: () => void;
  onConfirm: () => void;
  onRevoke: () => void;
  revoking: boolean;
  runner: Runner;
}) {
  const status = runnerStatus(runner);
  return (
    <li>
      <div className={styles.rowDetails}>
        <div className={styles.rowHeading}>
          <strong>{runner.name}</strong>
          <StatusBadge tone={statusTone[status]}>{copy.status[status]}</StatusBadge>
        </div>
        <div className={styles.pills} aria-label={copy.labels}>
          {runner.labels.map((label) => (
            <span className={styles.pill} key={label}>
              {label}
            </span>
          ))}
        </div>
        <div className={styles.rowMeta}>
          <span>
            {runner.lastSeenAt
              ? copy.lastSeen.replace("{relative}", formatRelativeTime(runner.lastSeenAt, locale))
              : copy.neverSeen}
          </span>
          {runner.runnerVersion && (
            <span>
              {copy.version} <code>{runner.runnerVersion}</code>
            </span>
          )}
          <span>{copy.added.replace("{date}", formatDate(runner.createdAt, locale))}</span>
        </div>
      </div>
      <div className={styles.rowActions}>
        {runner.revokedAt ? null : confirming ? (
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
