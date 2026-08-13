"use client";

import { HardDrive } from "lucide-react";
import { useState } from "react";

import { TokenRegistrationPanel } from "@/components/settings/token-registration-panel";
import { EmptyState } from "@/components/ui/empty-state";
import { StatusBadge } from "@/components/ui/status-badge";
import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession } from "@/lib/api-types";
import { formatDate, formatExpiryNote, formatRelativeTime } from "@/lib/format";
import { createLoreServerRegistrationToken, revokeLoreServer } from "@/lib/lore-server-client";
import {
  loreServerConfigureCommand,
  loreServerRunCommand,
  loreServerStatus,
  type LoreServer,
} from "@/lib/lore-servers";
import { mutationFailureMessage } from "@/lib/mutation-messages";

import styles from "@/components/settings/settings-panel.module.css";
import { DefaultLoreServerForm } from "./default-lore-server-form";

type LoreServerSettingsProps = {
  dictionary: Dictionary;
  initialDefaultServerID: string;
  initialServers: LoreServer[];
  locale: Locale;
  organization: string;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

type Registration = { token: string; expiresAt: string };

const statusTone = {
  active: "success",
  offline: "neutral",
  revoked: "danger",
} as const;

export function LoreServerSettings(props: LoreServerSettingsProps) {
  const copy = props.dictionary.loreServerSettings;
  const [servers, setServers] = useState(props.initialServers);
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
    const result = await createLoreServerRegistrationToken(props.organization, props.session.csrfToken);
    setCreating(false);
    if (!result.ok) {
      setError(result.kind === "invalid" ? copy.createFailed : mutationFailureMessage(result.kind, props.dictionary));
      return;
    }
    setOrigin(window.location.origin);
    setRegistration(result.data);
  }

  async function remove(server: LoreServer) {
    setRevoking(server.id);
    setError("");
    setNotice("");
    const result = await revokeLoreServer(props.organization, server.id, props.session.csrfToken);
    setRevoking("");
    setConfirming("");
    if (!result.ok) {
      setError(result.kind === "invalid" ? copy.revokeFailed : mutationFailureMessage(result.kind, props.dictionary));
      return;
    }
    setServers((current) =>
      current.map((item) =>
        item.id === server.id ? { ...item, status: "revoked", revokedAt: new Date().toISOString() } : item,
      ),
    );
    setNotice(copy.revoked);
  }

  return (
    <div className={styles.settings}>
      <div className={styles.toolbar}>
        <p>{copy.description}</p>
        <button className={styles.primaryButton} disabled={creating} onClick={() => void createToken()} type="button">
          {creating ? copy.creating : copy.newServer}
        </button>
      </div>
      {registration && (
        <TokenRegistrationPanel
          body={copy.registrationBody}
          commands={[loreServerConfigureCommand(origin), loreServerRunCommand()]}
          copiedLabel={copy.copied}
          copyCommandLabel={copy.copyCommand}
          copyTokenLabel={copy.copyToken}
          dismissLabel={copy.dismiss}
          expiryNote={formatExpiryNote(copy.tokenExpires, registration.expiresAt, props.locale)}
          hints={[copy.tokenOnce, copy.stdinNote]}
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
      {servers.length === 0 ? (
        <EmptyState body={copy.emptyBody} icon={<HardDrive aria-hidden="true" />} title={copy.empty} />
      ) : (
        <ul className={styles.list}>
          {servers.map((server) => (
            <LoreServerRow
              confirming={confirming === server.id}
              copy={copy}
              key={server.id}
              locale={props.locale}
              onCancel={() => setConfirming("")}
              onConfirm={() => setConfirming(server.id)}
              onRevoke={() => void remove(server)}
              revoking={revoking === server.id}
              server={server}
            />
          ))}
        </ul>
      )}
      <DefaultLoreServerForm
        dictionary={props.dictionary}
        initialServerID={props.initialDefaultServerID}
        organization={props.organization}
        servers={servers}
        session={props.session}
      />
    </div>
  );
}

function LoreServerRow({
  confirming,
  copy,
  locale,
  onCancel,
  onConfirm,
  onRevoke,
  revoking,
  server,
}: {
  confirming: boolean;
  copy: Dictionary["loreServerSettings"];
  locale: Locale;
  onCancel: () => void;
  onConfirm: () => void;
  onRevoke: () => void;
  revoking: boolean;
  server: LoreServer;
}) {
  const status = loreServerStatus(server);
  return (
    <li>
      <div className={styles.rowDetails}>
        <div className={styles.rowHeading}>
          <strong>{server.name}</strong>
          <StatusBadge tone={statusTone[status]}>{copy.status[status]}</StatusBadge>
          {server.instanceScope && <span className={styles.pill}>{copy.instanceScope}</span>}
        </div>
        <div className={styles.rowMeta}>
          <code>{server.publicUrl}</code>
        </div>
        <div className={styles.rowMeta}>
          <span>
            {server.lastSeenAt
              ? copy.lastSeen.replace("{relative}", formatRelativeTime(server.lastSeenAt, locale))
              : copy.neverSeen}
          </span>
          {server.loreBuildVersion && (
            <span>
              {copy.buildVersion} <code>{server.loreBuildVersion}</code>
            </span>
          )}
          <span>{copy.added.replace("{date}", formatDate(server.createdAt, locale))}</span>
        </div>
      </div>
      <div className={styles.rowActions}>
        {server.instanceScope || status === "revoked" ? null : confirming ? (
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
