"use client";

import { Check, X } from "lucide-react";
import Link from "next/link";
import { useEffect, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession } from "@/lib/api-types";
import {
  listRepositoryInvitations,
  repositoryInvitationAccountPath,
  repositoryInvitationPageSize,
  respondRepositoryInvitation,
  type RepositoryInvitation,
  type RepositoryInvitationPage,
} from "@/lib/repository-invitations";
import { repositoryPath } from "@/lib/routes";

import styles from "../repositories/repository-invitation-settings.module.css";

type RepositoryInvitationSettingsProps = {
  dictionary: Dictionary;
  locale: Locale;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

type LoadState = "loading" | "ready" | "forbidden" | "unavailable";

export function RepositoryInvitationSettings(props: RepositoryInvitationSettingsProps) {
  const copy = props.dictionary.repositoryInvitations;
  const [currentPage, setCurrentPage] = useState(1);
  const [page, setPage] = useState<RepositoryInvitationPage | null>(null);
  const [loadState, setLoadState] = useState<LoadState>("loading");
  const [pendingID, setPendingID] = useState<string | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    async function load() {
      const result = await listRepositoryInvitations(
        repositoryInvitationAccountPath(currentPage),
        currentPage,
        controller.signal,
      );
      if (controller.signal.aborted) return;
      if (!result.ok) {
        setLoadState(result.kind === "forbidden" ? "forbidden" : "unavailable");
        return;
      }
      if (result.data.total > 0 && result.data.invitations.length === 0 && currentPage > 1) {
        setCurrentPage(Math.max(1, Math.ceil(result.data.total / repositoryInvitationPageSize)));
        return;
      }
      setPage(result.data);
      setLoadState("ready");
    }
    void load();
    return () => controller.abort();
  }, [currentPage]);

  async function respond(invitation: RepositoryInvitation, response: "accept" | "decline") {
    setPendingID(invitation.id);
    setError("");
    const result = await respondRepositoryInvitation(invitation.id, response, props.session.csrfToken);
    setPendingID(null);
    if (!result.ok) {
      setError(
        result.kind === "conflict" ? copy.conflict : result.kind === "forbidden" ? copy.forbidden : copy.saveFailed,
      );
      return;
    }
    setPage((current) =>
      current
        ? {
            ...current,
            invitations: current.invitations.map((item) => (item.id === invitation.id ? result.data : item)),
          }
        : current,
    );
  }

  function changePage(value: number) {
    setLoadState("loading");
    setError("");
    setCurrentPage(value);
  }

  if (loadState === "loading") return <p className={styles.loading}>{copy.loading}</p>;
  if (loadState === "forbidden" || loadState === "unavailable" || page === null) {
    return (
      <p className={styles.error} role="alert">
        {loadState === "forbidden" ? copy.forbidden : copy.loadFailed}
      </p>
    );
  }

  const totalPages = Math.max(1, Math.ceil(page.total / page.perPage));
  return (
    <div className={styles.settings} id="repository-invitations">
      {error && (
        <p className={styles.error} role="alert">
          {error}
        </p>
      )}
      {page.invitations.length === 0 ? (
        <p className={styles.empty}>{copy.emptyAccount}</p>
      ) : (
        <ul className={styles.list}>
          {page.invitations.map((invitation) => (
            <li key={invitation.id}>
              <InvitationDetails copy={copy} invitation={invitation} locale={props.locale} />
              {invitation.status === "pending" && (
                <div className={styles.actions}>
                  <button
                    className={styles.primaryButton}
                    disabled={pendingID === invitation.id}
                    onClick={() => void respond(invitation, "accept")}
                    type="button"
                  >
                    <Check aria-hidden="true" size={15} />
                    {copy.accept}
                  </button>
                  <button
                    className={styles.secondaryButton}
                    disabled={pendingID === invitation.id}
                    onClick={() => void respond(invitation, "decline")}
                    type="button"
                  >
                    <X aria-hidden="true" size={15} />
                    {copy.decline}
                  </button>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
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

function InvitationDetails({
  copy,
  invitation,
  locale,
}: {
  copy: Dictionary["repositoryInvitations"];
  invitation: RepositoryInvitation;
  locale: Locale;
}) {
  const date = invitation.status === "pending" ? invitation.expiresAt : invitation.respondedAt;
  const dateLabel = invitation.status === "pending" ? copy.expires : copy.responded;
  return (
    <div className={styles.details}>
      <div className={styles.heading}>
        <Link href={repositoryPath(locale, invitation.owner, invitation.repository)}>
          {invitation.owner}/{invitation.repository}
        </Link>
        <span className={styles.status} data-status={invitation.status}>
          {copy.status[invitation.status]}
        </span>
        <code>{invitation.role}</code>
      </div>
      <span className={styles.metadata}>{copy.invitedBy.replace("{username}", invitation.invitedByUsername)}</span>
      {date && (
        <span className={styles.metadata}>{dateLabel.replace("{date}", formatInvitationDate(date, locale))}</span>
      )}
    </div>
  );
}

function formatInvitationDate(value: string, locale: Locale): string {
  return new Intl.DateTimeFormat(locale === "ja" ? "ja-JP" : "en-US", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}

function formatPageStatus(template: string, page: number, pages: number): string {
  return template.replace("{page}", String(page)).replace("{pages}", String(pages));
}
