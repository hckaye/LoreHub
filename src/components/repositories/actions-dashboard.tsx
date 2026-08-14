"use client";

import { ListChecks, Workflow } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { FormEvent, useMemo, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, CIRun, CIWorkflow, CIWorkflowDispatchInput, Deployment } from "@/lib/api-types";
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
  deployments: Deployment[];
  deploymentsAvailable: boolean;
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
  deployments,
  deploymentsAvailable,
}: ActionsDashboardProps) {
  const router = useRouter();
  const [selectedWorkflow, setSelectedWorkflow] = useState(workflows.find(isDispatchable)?.id ?? "");
  const [ref, setRef] = useState("");
  const [inputValues, setInputValues] = useState<Record<string, Record<string, string>>>(() =>
    Object.fromEntries(workflows.map((workflow) => [workflow.id, workflowInputDefaults(workflow)])),
  );
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [requiresLogin, setRequiresLogin] = useState(false);
  const dispatchableWorkflows = useMemo(() => workflows.filter(isDispatchable), [workflows]);
  const authenticated = session.status === "authenticated";
  const actionsBase = repositoryPath(locale, owner, repository, "actions");
  const selected = dispatchableWorkflows.find((workflow) => workflow.id === selectedWorkflow) ?? null;
  const dispatchInputs = selected?.triggerConfig.workflow_dispatch?.inputs ?? {};
  const selectedInputValues = inputValues[selectedWorkflow] ?? {};

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
      { ref: ref.trim(), inputs: selectedInputValues },
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
      <div className={styles.layout}>
        <aside className={styles.sidebar}>
          <WorkflowSection workflows={workflows} dictionary={dictionary} />
        </aside>
        <div className={styles.main}>
          <ActionsNotices failure={failure} requiresLogin={requiresLogin} dictionary={dictionary} />
          <RunSection
            runs={runs}
            actionsBase={actionsBase}
            canWrite={canWrite && authenticated}
            pending={pending}
            dictionary={dictionary}
            onMutate={mutateRun}
          />
          <DeploymentSection
            deployments={deployments}
            available={deploymentsAvailable}
            dictionary={dictionary}
            locale={locale}
            onReview={async (deployment, state) => {
              if (session.status !== "authenticated") {
                setRequiresLogin(true);
                return;
              }
              setPending(true);
              setFailure(null);
              const result = await postJson<Deployment>(
                actionsAPIPath(owner, repository, "deployments", deployment.id, "reviews"),
                { state },
                session.csrfToken,
              );
              setPending(false);
              if (!result.ok) {
                setFailure(mutationFailureMessage(result.kind, dictionary));
                return;
              }
              router.refresh();
            }}
            owner={owner}
            pending={pending}
            repository={repository}
          />
          {canWrite && authenticated && dispatchableWorkflows.length > 0 ? (
            <DispatchSection
              workflows={dispatchableWorkflows}
              selectedWorkflow={selectedWorkflow}
              selectedInputs={selectedInputValues}
              refValue={ref}
              inputs={dispatchInputs}
              pending={pending}
              dictionary={dictionary}
              onWorkflowChange={setSelectedWorkflow}
              onRefChange={setRef}
              onInputChange={(name, value) =>
                setInputValues((current) => ({
                  ...current,
                  [selectedWorkflow]: { ...current[selectedWorkflow], [name]: value },
                }))
              }
              onSubmit={dispatch}
            />
          ) : null}
        </div>
      </div>
    </div>
  );
}

function DeploymentSection({
  available,
  deployments,
  dictionary,
  locale,
  onReview,
  owner,
  pending,
  repository,
}: {
  available: boolean;
  deployments: Deployment[];
  dictionary: Dictionary;
  locale: Locale;
  onReview: (deployment: Deployment, state: "approved" | "rejected") => Promise<void>;
  owner: string;
  pending: boolean;
  repository: string;
}) {
  const copy = dictionary.actionsEnvironments;
  const actionsBase = repositoryPath(locale, owner, repository, "actions");
  return (
    <section className={styles.section}>
      <div className={styles.heading}>
        <div>
          <h2>{copy.deploymentsTitle}</h2>
          <p>{copy.deploymentsDescription}</p>
        </div>
      </div>
      {!available ? (
        <p className={styles.empty}>{copy.deploymentsUnavailable}</p>
      ) : deployments.length === 0 ? (
        <p className={styles.empty}>{copy.noDeployments}</p>
      ) : (
        <div className={styles.runList}>
          {deployments.map((deployment) => (
            <div className={styles.run} key={deployment.id}>
              <div className={styles.runMain}>
                <Link href={`${actionsBase}/${deployment.runNumber}`}>
                  {deployment.environmentName} · #{deployment.runNumber}
                </Link>
                <p>
                  {deployment.workflowName} · {deployment.branch} · {deployment.revision.slice(0, 12)}
                </p>
              </div>
              <StatusBadge tone={deploymentTone(deployment.status)}>
                {copy.deploymentStatuses[deployment.status]}
              </StatusBadge>
              {deployment.canReview && (
                <div className={styles.deploymentActions}>
                  <button disabled={pending} onClick={() => void onReview(deployment, "approved")} type="button">
                    {copy.approve}
                  </button>
                  <button disabled={pending} onClick={() => void onReview(deployment, "rejected")} type="button">
                    {copy.reject}
                  </button>
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

function deploymentTone(status: Deployment["status"]): "success" | "warning" | "danger" | "neutral" {
  if (status === "success") return "success";
  if (status === "failure" || status === "rejected") return "danger";
  if (status === "pending" || status === "waiting" || status === "queued") return "warning";
  return "neutral";
}

function ActionsNotices({
  failure,
  requiresLogin,
  dictionary,
}: {
  failure: string | null;
  requiresLogin: boolean;
  dictionary: Dictionary;
}) {
  return (
    <>
      {failure && <FlashNotice body={failure} title={dictionary.actionsPage.actionFailed} tone="error" />}
      {requiresLogin && <p className={styles.notice}>{dictionary.actionsPage.loginForMutation}</p>}
    </>
  );
}

function WorkflowSection({ workflows, dictionary }: { workflows: CIWorkflow[]; dictionary: Dictionary }) {
  return (
    <section className={styles.workflowSidebar}>
      <h2>
        <Workflow aria-hidden="true" size={16} />
        {dictionary.actionsPage.workflowsTitle}
      </h2>
      <div className={styles.workflowList}>
        <div aria-current="page" className={`${styles.workflow} ${styles.workflowSelected}`}>
          <ListChecks aria-hidden="true" size={16} />
          <strong>{dictionary.actionsPage.allWorkflows}</strong>
        </div>
        {workflows.map((workflow) => (
          <div className={styles.workflow} key={workflow.id}>
            <Workflow aria-hidden="true" size={16} />
            <div>
              <strong>{workflow.name}</strong>
              <code>{workflow.path}</code>
            </div>
            <StatusBadge tone={workflow.state === "active" ? "success" : "warning"}>
              {dictionary.actionsPage.states[workflow.state]}
            </StatusBadge>
          </div>
        ))}
        {workflows.length === 0 && <p className={styles.sidebarEmpty}>{dictionary.actionsPage.noWorkflows}</p>}
      </div>
    </section>
  );
}

type DispatchSectionProps = {
  workflows: CIWorkflow[];
  selectedWorkflow: string;
  selectedInputs: Record<string, string>;
  refValue: string;
  inputs: Record<string, CIWorkflowDispatchInput>;
  pending: boolean;
  dictionary: Dictionary;
  onWorkflowChange: (value: string) => void;
  onRefChange: (value: string) => void;
  onInputChange: (name: string, value: string) => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
};

function DispatchSection({
  workflows,
  selectedWorkflow,
  selectedInputs,
  refValue,
  inputs,
  pending,
  dictionary,
  onWorkflowChange,
  onRefChange,
  onInputChange,
  onSubmit,
}: DispatchSectionProps) {
  return (
    <section className={styles.section}>
      <div className={styles.heading}>
        <div>
          <h2>{dictionary.actionsPage.dispatchTitle}</h2>
          <p>{dictionary.actionsPage.dispatchDescription}</p>
        </div>
      </div>
      <form className={styles.dispatchForm} onSubmit={onSubmit}>
        <label>
          {dictionary.actionsPage.dispatchWorkflow}
          <select onChange={(event) => onWorkflowChange(event.target.value)} value={selectedWorkflow}>
            {workflows.map((workflow) => (
              <option key={workflow.id} value={workflow.id}>
                {workflow.name} · {workflow.path}
              </option>
            ))}
          </select>
        </label>
        <label>
          {dictionary.actionsPage.dispatchRef}
          <input
            onChange={(event) => onRefChange(event.target.value)}
            placeholder={dictionary.actionsPage.dispatchRefPlaceholder}
            required
            value={refValue}
          />
        </label>
        {Object.entries(inputs).map(([name, input]) => (
          <DispatchInput
            input={input}
            key={name}
            name={name}
            onChange={(value) => onInputChange(name, value)}
            value={selectedInputs[name] ?? ""}
          />
        ))}
        <button disabled={pending} type="submit">
          {pending ? dictionary.forms.submittingLabel : dictionary.actionsPage.dispatchButton}
        </button>
      </form>
    </section>
  );
}

type RunSectionProps = {
  runs: CIRun[];
  actionsBase: string;
  canWrite: boolean;
  pending: boolean;
  dictionary: Dictionary;
  onMutate: (run: CIRun, action: "cancel" | "rerun") => void;
};

function RunSection({ runs, actionsBase, canWrite, pending, dictionary, onMutate }: RunSectionProps) {
  return (
    <section className={styles.section}>
      <div className={styles.heading}>
        <div>
          <h2>{dictionary.actionsPage.runsTitle}</h2>
          <p>{dictionary.actionsPage.runsDescription}</p>
        </div>
      </div>
      {runs.length === 0 ? (
        <div className={styles.blankSlate}>
          <h3>{dictionary.actionsPage.noRunsTitle}</h3>
          <p>{dictionary.actionsPage.noRunsBody}</p>
        </div>
      ) : (
        <div className={styles.runList}>
          {runs.map((run) => (
            <RunRow
              key={run.id}
              run={run}
              actionsBase={actionsBase}
              canWrite={canWrite}
              pending={pending}
              dictionary={dictionary}
              onMutate={onMutate}
            />
          ))}
        </div>
      )}
    </section>
  );
}

function RunRow({
  run,
  actionsBase,
  canWrite,
  pending,
  dictionary,
  onMutate,
}: {
  run: CIRun;
  actionsBase: string;
  canWrite: boolean;
  pending: boolean;
  dictionary: Dictionary;
  onMutate: (run: CIRun, action: "cancel" | "rerun") => void;
}) {
  const active = run.status !== "completed" && run.status !== "cancelled";
  const finished = run.status === "completed" || run.status === "cancelled";
  return (
    <div className={styles.run}>
      <div className={styles.runIcon} aria-hidden="true">
        {run.conclusion === "success" ? "✓" : run.status === "in_progress" ? "…" : "!"}
      </div>
      <div className={styles.runMain}>
        <Link href={`${actionsBase}/${run.runNumber}`}>
          {dictionary.actionsPage.runNumber.replace("{number}", String(run.runNumber))}
        </Link>
        <p>
          {run.workflowName || run.workflowPath} · {dictionary.repository.event}: {run.eventName} · {run.branch}
        </p>
      </div>
      <StatusBadge tone={statusTone(run)}>{statusLabel(run, dictionary)}</StatusBadge>
      {canWrite && active && (
        <button disabled={pending} onClick={() => onMutate(run, "cancel")} type="button">
          {dictionary.actionsPage.cancelRun}
        </button>
      )}
      {canWrite && finished && (
        <button disabled={pending} onClick={() => onMutate(run, "rerun")} type="button">
          {dictionary.actionsPage.rerunRun}
        </button>
      )}
    </div>
  );
}

function isDispatchable(workflow: CIWorkflow): boolean {
  return workflow.enabled && workflow.state === "active" && Boolean(workflow.triggerConfig.workflow_dispatch);
}

function workflowInputDefaults(workflow: CIWorkflow): Record<string, string> {
  const inputs = workflow.triggerConfig.workflow_dispatch?.inputs ?? {};
  return Object.fromEntries(
    Object.entries(inputs).map(([name, input]) => [
      name,
      input.default ?? (input.type === "boolean" ? "false" : input.type === "choice" ? (input.options?.[0] ?? "") : ""),
    ]),
  );
}

function DispatchInput({
  input,
  name,
  onChange,
  value,
}: {
  input: CIWorkflowDispatchInput;
  name: string;
  onChange: (value: string) => void;
  value: string;
}) {
  const label = input.description ? `${name}: ${input.description}` : name;
  if (input.type === "choice") {
    return (
      <label>
        {label}
        <select onChange={(event) => onChange(event.target.value)} required={input.required} value={value}>
          {!input.required && <option value="" />}
          {(input.options ?? []).map((option) => (
            <option key={option} value={option}>
              {option}
            </option>
          ))}
        </select>
      </label>
    );
  }
  if (input.type === "boolean") {
    return (
      <label>
        <input
          checked={value === "true"}
          onChange={(event) => onChange(event.target.checked ? "true" : "false")}
          type="checkbox"
        />
        {label}
      </label>
    );
  }
  return (
    <label>
      {label}
      <input
        onChange={(event) => onChange(event.target.value)}
        required={input.required}
        type={input.type === "number" ? "number" : "text"}
        value={value}
      />
    </label>
  );
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
