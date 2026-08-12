"use client";

import { useEffect, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession } from "@/lib/api-types";
import {
  createRepositoryInvitation,
  listRepositoryCollaborators,
  listRepositoryInvitations,
  repositoryInvitationAdminPath,
  repositoryInvitationPageSize,
  repositoryInvitationRoles,
  revokeRepositoryCollaborator,
  revokeRepositoryInvitation,
  updateRepositoryCollaboratorRole,
  type RepositoryCollaborator,
  type RepositoryInvitation,
  type RepositoryInvitationPage,
  type RepositoryInvitationRole,
} from "@/lib/repository-invitations";

import { RepositoryCollaboratorList } from "./repository-collaborator-list";
import { RepositoryInvitationList } from "./repository-invitation-list";
import styles from "./repository-invitation-settings.module.css";

type RepositoryCollaboratorSettingsProps = {
  dictionary: Dictionary;
  locale: Locale;
  owner: string;
  repository: string;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

type LoadState = "loading" | "ready" | "forbidden" | "unavailable";

export function RepositoryCollaboratorSettings(props: RepositoryCollaboratorSettingsProps) {
  const copy = props.dictionary.repositoryInvitations;
  const [collaborators, setCollaborators] = useState<RepositoryCollaborator[]>([]);
  const [invitations, setInvitations] = useState<RepositoryInvitationPage | null>(null);
  const [currentPage, setCurrentPage] = useState(1);
  const [username, setUsername] = useState("");
  const [role, setRole] = useState<RepositoryInvitationRole>("read");
  const [loadState, setLoadState] = useState<LoadState>("loading");
  const [pendingID, setPendingID] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    async function load() {
      const [collaboratorResult, invitationResult] = await Promise.all([
        listRepositoryCollaborators(props.owner, props.repository, controller.signal),
        listRepositoryInvitations(
          repositoryInvitationAdminPath(props.owner, props.repository, currentPage),
          currentPage,
          controller.signal,
        ),
      ]);
      if (controller.signal.aborted) return;
      if (!collaboratorResult.ok || !invitationResult.ok) {
        const forbidden =
          (!collaboratorResult.ok && collaboratorResult.kind === "forbidden") ||
          (!invitationResult.ok && invitationResult.kind === "forbidden");
        setLoadState(forbidden ? "forbidden" : "unavailable");
        return;
      }
      if (invitationResult.data.total > 0 && invitationResult.data.invitations.length === 0 && currentPage > 1) {
        setCurrentPage(Math.max(1, Math.ceil(invitationResult.data.total / repositoryInvitationPageSize)));
        return;
      }
      setCollaborators(collaboratorResult.data.filter((collaborator) => collaborator.active));
      setInvitations(invitationResult.data);
      setLoadState("ready");
    }
    void load();
    return () => controller.abort();
  }, [currentPage, props.owner, props.repository]);

  async function invite(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const value = username.trim();
    if (!value) return;
    setPendingID("new");
    clearMessages();
    const result = await createRepositoryInvitation(
      props.owner,
      props.repository,
      value,
      role,
      props.session.csrfToken,
    );
    setPendingID(null);
    if (!result.ok) {
      setError(
        result.kind === "conflict" ? copy.conflict : result.kind === "forbidden" ? copy.forbidden : copy.saveFailed,
      );
      return;
    }
    setUsername("");
    setNotice(copy.sent);
    if (currentPage !== 1) {
      setLoadState("loading");
      setCurrentPage(1);
      return;
    }
    setInvitations((current) =>
      current
        ? {
            ...current,
            total: current.total + 1,
            invitations: [result.data, ...current.invitations].slice(0, current.perPage),
          }
        : current,
    );
  }

  async function changeRole(collaborator: RepositoryCollaborator, nextRole: RepositoryInvitationRole) {
    setPendingID(collaborator.userId);
    clearMessages();
    const result = await updateRepositoryCollaboratorRole(
      props.owner,
      props.repository,
      collaborator.username,
      nextRole,
      props.session.csrfToken,
    );
    setPendingID(null);
    if (!result.ok) {
      setError(
        result.kind === "conflict" ? copy.conflict : result.kind === "forbidden" ? copy.forbidden : copy.saveFailed,
      );
      return;
    }
    setCollaborators((current) => current.map((item) => (item.userId === collaborator.userId ? result.data : item)));
  }

  async function removeCollaborator(collaborator: RepositoryCollaborator) {
    setPendingID(collaborator.userId);
    clearMessages();
    const result = await revokeRepositoryCollaborator(
      props.owner,
      props.repository,
      collaborator.username,
      props.session.csrfToken,
    );
    setPendingID(null);
    if (!result.ok) {
      setError(result.kind === "forbidden" ? copy.forbidden : copy.saveFailed);
      return;
    }
    setCollaborators((current) => current.filter((item) => item.userId !== collaborator.userId));
  }

  async function revoke(invitation: RepositoryInvitation) {
    setPendingID(invitation.id);
    clearMessages();
    const result = await revokeRepositoryInvitation(
      props.owner,
      props.repository,
      invitation.id,
      props.session.csrfToken,
    );
    setPendingID(null);
    if (!result.ok) {
      setError(
        result.kind === "conflict" ? copy.conflict : result.kind === "forbidden" ? copy.forbidden : copy.saveFailed,
      );
      return;
    }
    setInvitations((current) =>
      current
        ? {
            ...current,
            invitations: current.invitations.map((item) =>
              item.id === invitation.id ? { ...item, status: "revoked", respondedAt: new Date().toISOString() } : item,
            ),
          }
        : current,
    );
  }

  function changePage(value: number) {
    clearMessages();
    setLoadState("loading");
    setCurrentPage(value);
  }

  function clearMessages() {
    setError("");
    setNotice("");
  }

  if (loadState === "loading") return <p className={styles.loading}>{copy.loading}</p>;
  if (loadState === "forbidden" || loadState === "unavailable" || invitations === null) {
    return (
      <p className={styles.error} role="alert">
        {loadState === "forbidden" ? copy.forbidden : copy.loadFailed}
      </p>
    );
  }

  const totalPages = Math.max(1, Math.ceil(invitations.total / invitations.perPage));
  return (
    <div className={styles.settings}>
      {error && (
        <p className={styles.error} role="alert">
          {error}
        </p>
      )}
      {notice && (
        <p className={styles.success} role="status">
          {notice}
        </p>
      )}
      <form className={styles.form} onSubmit={invite}>
        <label>
          <span>{copy.username}</span>
          <input maxLength={64} onChange={(event) => setUsername(event.target.value)} required value={username} />
        </label>
        <label>
          <span>{copy.role}</span>
          <select onChange={(event) => setRole(event.target.value as RepositoryInvitationRole)} value={role}>
            {repositoryInvitationRoles.map((value) => (
              <option key={value} value={value}>
                {value}
              </option>
            ))}
          </select>
        </label>
        <button className={styles.primaryButton} disabled={pendingID === "new" || !username.trim()} type="submit">
          {pendingID === "new" ? copy.inviting : copy.invite}
        </button>
      </form>
      <RepositoryCollaboratorList
        collaborators={collaborators}
        copy={copy}
        onRemove={removeCollaborator}
        onRoleChange={changeRole}
        pendingID={pendingID}
      />
      <RepositoryInvitationList
        copy={copy}
        invitations={invitations.invitations}
        locale={props.locale}
        onRevoke={revoke}
        pendingID={pendingID}
      />
      {totalPages > 1 && (
        <div aria-label={copy.title} className={styles.pagination}>
          <button
            className={styles.secondaryButton}
            disabled={currentPage <= 1}
            onClick={() => changePage(currentPage - 1)}
            type="button"
          >
            {copy.previousPage}
          </button>
          <span>{formatPageStatus(copy.pageStatus, currentPage, totalPages)}</span>
          <button
            className={styles.secondaryButton}
            disabled={currentPage >= totalPages}
            onClick={() => changePage(currentPage + 1)}
            type="button"
          >
            {copy.nextPage}
          </button>
        </div>
      )}
    </div>
  );
}

function formatPageStatus(template: string, page: number, pages: number): string {
  return template.replace("{page}", String(page)).replace("{pages}", String(pages));
}
