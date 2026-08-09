"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { FormEvent, useMemo, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, CIRun, CIWorkflow } from "@/lib/api-types";
import { postJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { actionsAPIPath, repositoryPath } from "@/lib/routes";

import { FlashNotice } from "../ui/flash-notice";
import { StatusBadge } from "../ui/status-badge";
import styles from "./actions-dashboard.module.css";

type ActionsDashboardProps = {
  locale: Locale;
  owner: string;
  repository: string;
  workflows: CIWorkflow[];
  runs: CIRun[];
  canWrite: boolean;
  session: AuthSession;
  dictionary: Dictionary;
};

export function ActionsDashboard({
  locale,
  owner,
  repository,
  workflows,
  runs,
  canWrite,
  session,
  dictionary,
}: ActionsDashboardProps) {
  const router = useRouter();
  const [selectedWorkflow, setSelectedWorkflow] = useState(workflows.find(isDispatchable)?.id ?? "");
  const [ref, setRef] = useState("");
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [requiresLogin, setRequiresLogin] = useState(false);
  const dispatchableWorkflows = useMemo(() => workflows.filter(isDispatchable), [workflows]);
  const authenticated = session.status === "authenticated";
  const actionsBase = repositoryPath(locale, owner, repository, "actions");

  async function dispatch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (session.status !== "authenticated" || !selectedWorkflow || !ref.trim()) {
      setFailure(dictionary.actionsPage.dispatchInvalid);
      return;
    }
    setPending(true);
    setFailure(null);
    setRequiresLogin(false);
    const result = await postJson<CIRun>(
      actionsAPIPath(owner, repository, "workflows", selectedWorkflow, "dispatches"),
      { ref: ref.trim() },
      session.csrfToken,
    );
    if (result.ok) {
      router.push(`${actionsBase}/${result.data.runNumber}`);
      router.refresh();
      return;
    }
    setPending(false);
    setFailure(mutationFailureMessage(result.kind, dictionary));
    setRequiresLogin(result.kind === "unauthorized");
  }

  async function mutateRun(run: CIRun, action: "cancel" | "rerun") {
    if (session.status !== "authenticated") {
      setRequiresLogin(true);
      return;
    }
    setPending(true);
    setFailure(null);
    const result = await postJson<CIRun>(
      actionsAPIPath(owner, repository, "runs", String(run.runNumber), action),
      {},
      session.csrfToken,
    );
    if (result.ok) {
      if (action === "rerun") {
        router.push(`${actionsBase}/${result.data.runNumber}`);
      } else {
        router.refresh();
      }
      return;
    }
    setPending(false);
    setFailure(mutationFailureMessage(result.kind, dictionary));
    setRequiresLogin(result.kind === "unauthorized");
  }

  return (
    <div className={styles.dashboard}>
      {failure && <FlashNotice body={failure} title={dictionary.actionsPage.actionFailed} tone="error" />}
      {requiresLogin && <p className={styles.notice}>{dictionary.actionsPage.loginForMutation}</p>}
      <section className={styles.section}>
        <div className={styles.heading}>
          <div>
            <h2>{dictionary.actionsPage.workflowsTitle}</h2>
            <p>{dictionary.actionsPage.workflowsDescription}</p>
          </div>
        </div>
        {workflows.length === 0 ? (
          <p className={styles.empty}>{dictionary.actionsPage.noWorkflows}</p>
        ) : (
          <div className={styles.workflowList}>
            {workflows.map((workflow) => (
              <div className={styles.workflow} key={workflow.id}>
                <div>
                  <strong>{workflow.name}</strong>
                  <code>{workflow.path}</code>
                </div>
                <StatusBadge tone={workflow.state === "active" ? "success" : "warning"}>
                  {dictionary.actionsPage.states[workflow.state]}
                </StatusBadge>
              </div>
            ))}
          </div>
        )}
      </section>
      {canWrite && authenticated && dispatchableWorkflows.length > 0 && (
        <section className={styles.section}>
          <div className={styles.heading}>
            <div>
              <h2>{dictionary.actionsPage.dispatchTitle}</h2>
              <p>{dictionary.actionsPage.dispatchDescription}</p>
            </div>
          </div>
          <form className={styles.dispatchForm} onSubmit={dispatch}>
            <label>
              {dictionary.actionsPage.dispatchWorkflow}
              <select onChange={(event) => setSelectedWorkflow(event.target.value)} value={selectedWorkflow}>
                {dispatchableWorkflows.map((workflow) => (
                  <option key={workflow.id} value={workflow.id}>
                    {workflow.name} · {workflow.path}
                  </option>
                ))}
              </select>
            </label>
            <label>
              {dictionary.actionsPage.dispatchRef}
              <input
                onChange={(event) => setRef(event.target.value)}
                placeholder={dictionary.actionsPage.dispatchRefPlaceholder}
                required
                value={ref}
              />
            </label>
            <button disabled={pending} type="submit">
              {pending ? dictionary.forms.submittingLabel : dictionary.actionsPage.dispatchButton}
            </button>
          </form>
        </section>
      )}
      <section className={styles.section}>
        <div className={styles.heading}>
          <div>
            <h2>{dictionary.actionsPage.runsTitle}</h2>
            <p>{dictionary.actionsPage.runsDescription}</p>
          </div>
        </div>
        {runs.length === 0 ? (
          <p className={styles.empty}>{dictionary.actionsPage.noRunsBody}</p>
        ) : (
          <div className={styles.runList}>
            {runs.map((run) => (
              <div className={styles.run} key={run.id}>
                <div className={styles.runIcon} aria-hidden="true">
                  {run.conclusion === "success" ? "✓" : run.status === "in_progress" ? "…" : "!"}
                </div>
                <div className={styles.runMain}>
                  <Link href={`${actionsBase}/${run.runNumber}`}>
                    {dictionary.actionsPage.runNumber.replace("{number}", String(run.runNumber))}
                  </Link>
                  <p>
                    {run.workflowName || run.workflowPath} · {dictionary.repository.event}: {run.eventName} ·{" "}
                    {run.branch}
                  </p>
                </div>
                <StatusBadge tone={statusTone(run)}>{statusLabel(run, dictionary)}</StatusBadge>
                {canWrite && authenticated && run.status !== "completed" && run.status !== "cancelled" && (
                  <button disabled={pending} onClick={() => mutateRun(run, "cancel")} type="button">
                    {dictionary.actionsPage.cancelRun}
                  </button>
                )}
                {canWrite && authenticated && (run.status === "completed" || run.status === "cancelled") && (
                  <button disabled={pending} onClick={() => mutateRun(run, "rerun")} type="button">
                    {dictionary.actionsPage.rerunRun}
                  </button>
                )}
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}

function isDispatchable(workflow: CIWorkflow): boolean {
  return workflow.enabled && workflow.state === "active" && Boolean(workflow.triggerConfig.workflow_dispatch);
}

function statusLabel(run: CIRun, dictionary: Dictionary): string {
  const key = run.conclusion ?? run.status;
  return (
    dictionary.actionsPage.statuses[key as keyof typeof dictionary.actionsPage.statuses] ??
    dictionary.actionsPage.statuses.unknown
  );
}

function statusTone(run: CIRun): "neutral" | "success" | "warning" | "danger" {
  if (run.conclusion === "success") return "success";
  if (run.conclusion && run.conclusion !== "skipped") return "danger";
  if (run.status === "queued" || run.status === "in_progress") return "warning";
  return "neutral";
}
