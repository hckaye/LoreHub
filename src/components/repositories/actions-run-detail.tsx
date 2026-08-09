"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, CIRunDetail } from "@/lib/api-types";
import { postJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { actionsAPIPath, repositoryPath } from "@/lib/routes";

import { FlashNotice } from "../ui/flash-notice";
import { StatusBadge } from "../ui/status-badge";
import styles from "./actions-run-detail.module.css";

type ActionsRunDetailProps = {
  locale: "en" | "ja";
  owner: string;
  repository: string;
  detail: CIRunDetail;
  session: AuthSession;
  canWrite: boolean;
  dictionary: Dictionary;
};

export function ActionsRunDetail({
  locale,
  owner,
  repository,
  detail,
  session,
  canWrite,
  dictionary,
}: ActionsRunDetailProps) {
  const router = useRouter();
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const run = detail.run;
  const authenticated = session.status === "authenticated";
  const basePath = repositoryPath(locale, owner, repository, "actions");

  async function mutate(action: "cancel" | "rerun") {
    if (session.status !== "authenticated") return;
    setPending(true);
    setFailure(null);
    const result = await postJson(
      actionsAPIPath(owner, repository, "runs", String(run.runNumber), action),
      {},
      session.csrfToken,
    );
    if (result.ok) {
      router.refresh();
      return;
    }
    setPending(false);
    setFailure(mutationFailureMessage(result.kind, dictionary));
  }

  return (
    <div className={styles.page}>
      {failure && <FlashNotice body={failure} title={dictionary.actionsPage.actionFailed} tone="error" />}
      <div className={styles.toolbar}>
        <Link href={basePath}>{dictionary.actionsPage.backToActions}</Link>
        {canWrite && authenticated && run.status !== "completed" && run.status !== "cancelled" && (
          <button disabled={pending} onClick={() => mutate("cancel")} type="button">
            {dictionary.actionsPage.cancelRun}
          </button>
        )}
        {canWrite && authenticated && (run.status === "completed" || run.status === "cancelled") && (
          <button disabled={pending} onClick={() => mutate("rerun")} type="button">
            {dictionary.actionsPage.rerunRun}
          </button>
        )}
      </div>
      <section className={styles.summary}>
        <div>
          <p className={styles.eyebrow}>{detail.workflow.path}</p>
          <h2>{dictionary.actionsPage.runNumber.replace("{number}", String(run.runNumber))}</h2>
          <p>
            {detail.workflow.name} · {run.eventName} · {run.branch}
          </p>
        </div>
        <StatusBadge tone={statusTone(run)}>{statusLabel(run, dictionary)}</StatusBadge>
      </section>
      <section className={styles.section}>
        <h2>{dictionary.actionsPage.jobsTitle}</h2>
        <div className={styles.list}>
          {detail.jobs.map((job) => (
            <div className={styles.row} key={job.id}>
              <div>
                <strong>{job.name}</strong>
                <p>
                  {dictionary.actionsPage.attempt}: {job.attempt} · {dictionary.actionsPage.jobStatus}:{" "}
                  {statusLabel(job.status, dictionary)}
                </p>
              </div>
              {job.logAvailable && (
                <a href={actionsAPIPath(owner, repository, "jobs", job.id, "logs")}>{dictionary.actionsPage.viewLog}</a>
              )}
            </div>
          ))}
        </div>
      </section>
      <section className={styles.section}>
        <h2>{dictionary.actionsPage.artifactsTitle}</h2>
        {detail.artifacts.length === 0 ? (
          <p className={styles.empty}>{dictionary.actionsPage.noArtifacts}</p>
        ) : (
          <div className={styles.list}>
            {detail.artifacts.map((artifact) => (
              <div className={styles.row} key={artifact.id}>
                <div>
                  <strong>{artifact.name}</strong>
                  <p>{formatBytes(artifact.sizeBytes)}</p>
                </div>
                <a href={actionsAPIPath(owner, repository, "artifacts", artifact.id, "download")}>
                  {dictionary.actionsPage.downloadArtifact}
                </a>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}

function statusLabel(value: string | CIRunDetail["run"], dictionary: Dictionary): string {
  const key = typeof value === "string" ? value : (value.conclusion ?? value.status);
  return (
    dictionary.actionsPage.statuses[key as keyof typeof dictionary.actionsPage.statuses] ??
    dictionary.actionsPage.statuses.unknown
  );
}

function statusTone(run: CIRunDetail["run"]): "neutral" | "success" | "warning" | "danger" {
  if (run.conclusion === "success") return "success";
  if (run.conclusion && run.conclusion !== "skipped") return "danger";
  if (run.status === "queued" || run.status === "in_progress") return "warning";
  return "neutral";
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KiB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
}
