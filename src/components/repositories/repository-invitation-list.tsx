import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import { formatDateTime } from "@/lib/format";
import type { RepositoryInvitation } from "@/lib/repository-invitations";

import styles from "./repository-invitation-settings.module.css";

type RepositoryInvitationListProps = {
  copy: Dictionary["repositoryInvitations"];
  invitations: RepositoryInvitation[];
  locale: Locale;
  onRevoke: (invitation: RepositoryInvitation) => Promise<void>;
  pendingID: string | null;
};

export function RepositoryInvitationList(props: RepositoryInvitationListProps) {
  if (props.invitations.length === 0) {
    return <p className={styles.empty}>{props.copy.emptyAdministrator}</p>;
  }
  return (
    <ul className={styles.list}>
      {props.invitations.map((invitation) => (
        <li key={invitation.id}>
          <div className={styles.details}>
            <div className={styles.heading}>
              <strong>{invitation.inviteeDisplayName || invitation.inviteeUsername}</strong>
              <span className={styles.status} data-status={invitation.status}>
                {props.copy.status[invitation.status]}
              </span>
              <code>{invitation.role}</code>
            </div>
            <span className={styles.metadata}>@{invitation.inviteeUsername}</span>
            <span className={styles.metadata}>
              {props.copy.expires.replace("{date}", formatDateTime(invitation.expiresAt, props.locale))}
            </span>
          </div>
          {invitation.status === "pending" && (
            <button
              className={styles.dangerButton}
              disabled={props.pendingID === invitation.id}
              onClick={() => void props.onRevoke(invitation)}
              type="button"
            >
              {props.copy.revoke}
            </button>
          )}
        </li>
      ))}
    </ul>
  );
}
