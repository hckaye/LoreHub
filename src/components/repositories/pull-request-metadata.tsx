"use client";

import { useRouter } from "next/navigation";
import { useState, type CSSProperties, type ReactNode } from "react";

import type { Dictionary } from "@/i18n";
import type { Assignee, Label, MergeRequestMetadata, Milestone } from "@/lib/api-types";
import { deleteJson, putJson } from "@/lib/auth-client";

import { UserAvatar } from "../ui/user-avatar";
import styles from "./pull-request-metadata.module.css";

type PullRequestMetadataProps = {
  assignees: Assignee[];
  assigneesAvailable: boolean;
  csrfToken: string;
  dictionary: Dictionary;
  labels: Label[];
  labelsAvailable: boolean;
  metadata: MergeRequestMetadata | null;
  milestones: Milestone[];
  milestonesAvailable: boolean;
  number: number;
  owner: string;
  repository: string;
};

export function PullRequestMetadata(props: PullRequestMetadataProps) {
  const router = useRouter();
  const [busy, setBusy] = useState<string | null>(null);
  const [message, setMessage] = useState("");
  const copy = props.dictionary.pullRequestMetadata;
  const path = metadataPath(props.owner, props.repository, props.number);

  async function mutate(action: string, request: () => ReturnType<typeof deleteJson<unknown>>) {
    if (!props.csrfToken) return;
    setBusy(action);
    setMessage("");
    const result = await request();
    setBusy(null);
    if (!result.ok) {
      setMessage(copy.updateFailed);
      return;
    }
    router.refresh();
  }

  return (
    <aside aria-label={copy.title} className={styles.card}>
      {message && (
        <p aria-live="polite" className={styles.error} role="alert">
          {message}
        </p>
      )}
      <Assignees {...props} busy={busy} mutate={mutate} path={path} />
      <Labels {...props} busy={busy} mutate={mutate} path={path} />
      <Milestone {...props} busy={busy} mutate={mutate} path={path} />
    </aside>
  );
}

type SectionProps = PullRequestMetadataProps & {
  busy: string | null;
  mutate(action: string, request: () => ReturnType<typeof deleteJson<unknown>>): Promise<void>;
  path: string;
};

function Assignees(props: SectionProps) {
  const copy = props.dictionary.pullRequestMetadata;
  const assigned = new Set(props.metadata?.assignees.map((user) => user.id) ?? []);
  const candidates = mergeAssignees(props.metadata?.assignees ?? [], props.assignees);
  return (
    <section>
      <SectionHeading label={copy.assignees}>
        {props.metadata?.viewerCanManageAssignees && props.assigneesAvailable && (
          <details>
            <summary>{copy.manageAssignees}</summary>
            <div className={styles.menu}>
              {candidates.length > 0 ? (
                candidates.map((user) => {
                  const action = `assignee:${user.id}`;
                  const userPath = `${props.path}/assignees/${encodeURIComponent(user.username)}`;
                  return (
                    <label key={user.id}>
                      <input
                        checked={assigned.has(user.id)}
                        disabled={props.busy === action}
                        onChange={(event) =>
                          props.mutate(action, () =>
                            event.target.checked
                              ? putJson(userPath, undefined, props.csrfToken)
                              : deleteJson(userPath, props.csrfToken),
                          )
                        }
                        type="checkbox"
                      />
                      <AssigneeIdentity assignee={user} />
                    </label>
                  );
                })
              ) : (
                <p>{copy.noCandidates}</p>
              )}
            </div>
          </details>
        )}
      </SectionHeading>
      {props.metadata === null ? (
        <p>{copy.assigneesUnavailable}</p>
      ) : props.metadata.assignees.length > 0 ? (
        <div className={styles.assignees}>
          {props.metadata.assignees.map((user) => (
            <AssigneeIdentity assignee={user} key={user.id} />
          ))}
        </div>
      ) : (
        <p>{copy.noAssignees}</p>
      )}
      {props.metadata?.viewerCanManageAssignees && !props.assigneesAvailable && <p>{copy.assigneesUnavailable}</p>}
    </section>
  );
}

function Labels(props: SectionProps) {
  const copy = props.dictionary.pullRequestMetadata;
  const selected = new Set(props.metadata?.labels.map((label) => label.id) ?? []);
  return (
    <section>
      <SectionHeading label={copy.labels}>
        {props.metadata?.viewerCanManageLabels && props.labelsAvailable && (
          <details>
            <summary>{copy.manageLabels}</summary>
            <div className={styles.menu}>
              {props.labels.map((label) => {
                const labelPath = `${props.path}/labels/${encodeURIComponent(label.id)}`;
                return (
                  <label key={label.id}>
                    <input
                      checked={selected.has(label.id)}
                      disabled={props.busy === `label:${label.id}`}
                      onChange={(event) =>
                        props.mutate(`label:${label.id}`, () =>
                          event.target.checked
                            ? putJson(labelPath, undefined, props.csrfToken)
                            : deleteJson(labelPath, props.csrfToken),
                        )
                      }
                      type="checkbox"
                    />
                    <LabelChip label={label} />
                  </label>
                );
              })}
            </div>
          </details>
        )}
      </SectionHeading>
      {props.metadata === null ? (
        <p>{copy.labelsUnavailable}</p>
      ) : props.metadata.labels.length > 0 ? (
        <div className={styles.labels}>
          {props.metadata.labels.map((label) => (
            <LabelChip key={label.id} label={label} />
          ))}
        </div>
      ) : (
        <p>{copy.noLabels}</p>
      )}
      {props.metadata?.viewerCanManageLabels && !props.labelsAvailable && <p>{copy.labelsUnavailable}</p>}
    </section>
  );
}

function Milestone(props: SectionProps) {
  const copy = props.dictionary.pullRequestMetadata;
  return (
    <section>
      <SectionHeading label={copy.milestone}>
        {props.metadata?.viewerCanManageMilestone && props.milestonesAvailable && (
          <details>
            <summary>{copy.manageMilestone}</summary>
            <div className={styles.milestoneMenu}>
              <button
                aria-pressed={props.metadata.milestone === null}
                disabled={props.busy === "milestone"}
                onClick={() => props.mutate("milestone", () => deleteJson(`${props.path}/milestone`, props.csrfToken))}
                type="button"
              >
                {copy.removeMilestone}
              </button>
              {props.milestones.map((milestone) => (
                <button
                  aria-pressed={props.metadata?.milestone?.id === milestone.id}
                  disabled={props.busy === "milestone"}
                  key={milestone.id}
                  onClick={() =>
                    props.mutate("milestone", () =>
                      putJson(`${props.path}/milestone/${milestone.number}`, undefined, props.csrfToken),
                    )
                  }
                  type="button"
                >
                  <span>{milestone.title}</span>
                  <small>
                    {milestone.state === "open" ? props.dictionary.common.open : props.dictionary.common.closed}
                  </small>
                </button>
              ))}
            </div>
          </details>
        )}
      </SectionHeading>
      {props.metadata === null ? (
        <p>{copy.milestonesUnavailable}</p>
      ) : props.metadata.milestone ? (
        <div className={styles.milestone}>
          <strong>{props.metadata.milestone.title}</strong>
          {props.metadata.milestone.dueOn && <span>{props.metadata.milestone.dueOn}</span>}
        </div>
      ) : (
        <p>{copy.noMilestone}</p>
      )}
      {props.metadata?.viewerCanManageMilestone && !props.milestonesAvailable && <p>{copy.milestonesUnavailable}</p>}
    </section>
  );
}

function SectionHeading({ children, label }: { children: ReactNode; label: string }) {
  return (
    <div className={styles.heading}>
      <h3>{label}</h3>
      {children}
    </div>
  );
}

function AssigneeIdentity({ assignee }: { assignee: Assignee }) {
  return (
    <span className={styles.assignee}>
      <UserAvatar avatarUrl={assignee.avatarUrl} name={assignee.displayName || assignee.username} size={26} />
      <span>
        <strong>{assignee.displayName || assignee.username}</strong>
        <small>@{assignee.username}</small>
      </span>
    </span>
  );
}

function LabelChip({ label }: { label: Label }) {
  return (
    <span className={styles.label} style={{ "--label-color": safeColor(label.color) } as CSSProperties}>
      {label.name}
    </span>
  );
}

function mergeAssignees(current: Assignee[], candidates: Assignee[]): Assignee[] {
  const users = new Map<string, Assignee>();
  for (const user of [...current, ...candidates]) users.set(user.id, user);
  return [...users.values()];
}

function safeColor(value: string): string {
  return /^#[0-9a-f]{6}$/i.test(value) ? value : /^([0-9a-f]{6})$/i.test(value) ? `#${value}` : "#6e7781";
}

function metadataPath(owner: string, repository: string, number: number): string {
  return [
    "/api/v1/repositories",
    encodeURIComponent(owner),
    encodeURIComponent(repository),
    "merge-requests",
    number,
    "metadata",
  ].join("/");
}
