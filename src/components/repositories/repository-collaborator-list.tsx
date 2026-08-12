import type { Dictionary } from "@/i18n";
import {
  repositoryInvitationRoles,
  type RepositoryCollaborator,
  type RepositoryInvitationRole,
} from "@/lib/repository-invitations";

import styles from "./repository-invitation-settings.module.css";

type RepositoryCollaboratorListProps = {
  collaborators: RepositoryCollaborator[];
  copy: Dictionary["repositoryInvitations"];
  onRemove: (collaborator: RepositoryCollaborator) => Promise<void>;
  onRoleChange: (collaborator: RepositoryCollaborator, role: RepositoryInvitationRole) => Promise<void>;
  pendingID: string | null;
};

export function RepositoryCollaboratorList(props: RepositoryCollaboratorListProps) {
  if (props.collaborators.length === 0) return null;
  return (
    <ul className={styles.list}>
      {props.collaborators.map((collaborator) => (
        <li key={`${collaborator.source}-${collaborator.userId}`}>
          <div className={styles.details}>
            <div className={styles.heading}>
              <strong>{collaborator.displayName || collaborator.username}</strong>
              <span className={styles.status}>{collaborator.source}</span>
            </div>
            <span className={styles.metadata}>@{collaborator.username}</span>
          </div>
          <div className={styles.actions}>
            {collaborator.source === "direct" ? (
              <>
                <select
                  aria-label={`${props.copy.role}: ${collaborator.username}`}
                  className={styles.roleSelect}
                  disabled={props.pendingID === collaborator.userId}
                  onChange={(event) =>
                    void props.onRoleChange(collaborator, event.target.value as RepositoryInvitationRole)
                  }
                  value={collaborator.role}
                >
                  {repositoryInvitationRoles.map((role) => (
                    <option key={role} value={role}>
                      {role}
                    </option>
                  ))}
                </select>
                <button
                  className={styles.dangerButton}
                  disabled={props.pendingID === collaborator.userId}
                  onClick={() => void props.onRemove(collaborator)}
                  type="button"
                >
                  {props.copy.removeAccess}
                </button>
              </>
            ) : (
              <code>{collaborator.role}</code>
            )}
          </div>
        </li>
      ))}
    </ul>
  );
}
