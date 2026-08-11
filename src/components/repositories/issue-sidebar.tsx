"use client";

import { useEffect, useState, type CSSProperties } from "react";

import type { Dictionary } from "@/i18n";
import type { Assignee, Issue, Label, Milestone } from "@/lib/api-types";
import { searchIssueAssignees } from "@/lib/issue-assignee-client";

import styles from "./issue-detail.module.css";

type IssueSidebarProps = {
  busyAction: string | null;
  dictionary: Dictionary;
  issue: Issue;
  labels: Label[];
  labelsAvailable: boolean;
  milestones: Milestone[];
  milestonesAvailable: boolean;
  owner: string;
  repository: string;
  assignableUsers: Assignee[];
  assigneesAvailable: boolean;
  onSetAssignee: (assignee: Assignee, selected: boolean) => Promise<void>;
  onSetMilestone: (number: number | null) => Promise<void>;
  onToggleLabel: (label: Label, selected: boolean) => Promise<void>;
  onUpdateState: (state: "open" | "closed") => Promise<boolean>;
};

export function IssueSidebar(props: IssueSidebarProps) {
  const selected = new Set(props.issue.labels.map((label) => label.id));
  return (
    <aside className={styles.sidebar}>
      <AssigneeSection {...props} />
      <section>
        <div className={styles.sidebarHeading}>
          <h2>{props.dictionary.issueDetail.labels}</h2>
          {props.issue.viewerCanManageLabels && props.labelsAvailable && (
            <details>
              <summary>{props.dictionary.issueDetail.manageLabels}</summary>
              <div className={styles.labelMenu}>
                {props.labels.map((label) => (
                  <label key={label.id}>
                    <input
                      checked={selected.has(label.id)}
                      disabled={props.busyAction === label.id}
                      onChange={(event) => props.onToggleLabel(label, event.target.checked)}
                      type="checkbox"
                    />
                    <LabelChip label={label} />
                  </label>
                ))}
              </div>
            </details>
          )}
        </div>
        {props.issue.labels.length > 0 ? (
          <div className={styles.labels}>
            {props.issue.labels.map((label) => (
              <LabelChip key={label.id} label={label} />
            ))}
          </div>
        ) : (
          <p>{props.dictionary.issueDetail.noLabels}</p>
        )}
        {!props.labelsAvailable && <p>{props.dictionary.issueDetail.labelsUnavailable}</p>}
      </section>
      <section>
        <div className={styles.sidebarHeading}>
          <h2>{props.dictionary.milestonesPage.assignTitle}</h2>
          {props.issue.viewerCanManageMilestone && props.milestonesAvailable && (
            <details>
              <summary>{props.dictionary.milestonesPage.manageAssignment}</summary>
              <div className={styles.milestoneMenu}>
                <button
                  disabled={props.busyAction === "milestone"}
                  onClick={() => props.onSetMilestone(null)}
                  type="button"
                >
                  {props.dictionary.milestonesPage.removeAssignment}
                </button>
                {props.milestones.map((milestone) => (
                  <button
                    aria-pressed={props.issue.milestone?.id === milestone.id}
                    disabled={props.busyAction === "milestone"}
                    key={milestone.id}
                    onClick={() => props.onSetMilestone(milestone.number)}
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
        </div>
        {props.issue.milestone ? (
          <div className={styles.milestoneValue}>
            <strong>{props.issue.milestone.title}</strong>
            {props.issue.milestone.dueOn && <span>{props.issue.milestone.dueOn}</span>}
          </div>
        ) : (
          <p>{props.dictionary.milestonesPage.noAssignment}</p>
        )}
        {!props.milestonesAvailable && <p>{props.dictionary.milestonesPage.assignmentUnavailable}</p>}
      </section>
      {props.issue.viewerCanUpdate && (
        <button
          className={styles.stateButton}
          data-state={props.issue.state}
          disabled={props.busyAction === "issue"}
          onClick={() => props.onUpdateState(props.issue.state === "open" ? "closed" : "open")}
          type="button"
        >
          {props.issue.state === "open"
            ? props.dictionary.issueDetail.closeIssue
            : props.dictionary.issueDetail.reopenIssue}
        </button>
      )}
    </aside>
  );
}

function AssigneeSection(props: IssueSidebarProps) {
  const assignedUsers = new Set(props.issue.assignees.map((assignee) => assignee.id));
  const [query, setQuery] = useState("");
  const [searchResults, setSearchResults] = useState<Assignee[] | null>(null);
  const normalizedQuery = query.trim().toLowerCase();
  const candidates = normalizedQuery && searchResults ? searchResults : props.assignableUsers;
  const options = mergeAssignees(props.issue.assignees, candidates).filter((assignee) => {
    return !normalizedQuery || `${assignee.displayName} ${assignee.username}`.toLowerCase().includes(normalizedQuery);
  });
  useEffect(() => {
    if (!normalizedQuery) return;
    const controller = new AbortController();
    const timer = window.setTimeout(async () => {
      const result = await searchIssueAssignees(props.owner, props.repository, normalizedQuery, controller.signal);
      if (result.ok) setSearchResults(result.data.items);
    }, 200);
    return () => {
      controller.abort();
      window.clearTimeout(timer);
    };
  }, [normalizedQuery, props.owner, props.repository]);
  return (
    <section>
      <div className={styles.sidebarHeading}>
        <h2>{props.dictionary.issueAssignees.title}</h2>
        {props.issue.viewerCanManageAssignees && props.assigneesAvailable && (
          <details>
            <summary>{props.dictionary.issueAssignees.manage}</summary>
            <div className={styles.assigneeMenu}>
              <input
                aria-label={props.dictionary.issueAssignees.searchLabel}
                onChange={(event) => setQuery(event.target.value)}
                placeholder={props.dictionary.issueAssignees.searchPlaceholder}
                type="search"
                value={query}
              />
              {options.length > 0 ? (
                options.map((assignee) => (
                  <label key={assignee.id}>
                    <input
                      checked={assignedUsers.has(assignee.id)}
                      disabled={props.busyAction === `assignee:${assignee.id}`}
                      onChange={(event) => props.onSetAssignee(assignee, event.target.checked)}
                      type="checkbox"
                    />
                    <AssigneeIdentity assignee={assignee} />
                  </label>
                ))
              ) : (
                <p>{props.dictionary.issueAssignees.noCandidates}</p>
              )}
            </div>
          </details>
        )}
      </div>
      {props.issue.assignees.length > 0 ? (
        <div className={styles.assigneeList}>
          {props.issue.assignees.map((assignee) => (
            <AssigneeIdentity assignee={assignee} key={assignee.id} />
          ))}
        </div>
      ) : (
        <p>{props.dictionary.issueAssignees.empty}</p>
      )}
      {props.issue.viewerCanManageAssignees && !props.assigneesAvailable && (
        <p>{props.dictionary.issueAssignees.unavailable}</p>
      )}
    </section>
  );
}

function AssigneeIdentity({ assignee }: { assignee: Assignee }) {
  const initial = [...(assignee.displayName || assignee.username)][0]?.toUpperCase() ?? "?";
  return (
    <span className={styles.assigneeIdentity}>
      <span aria-hidden="true" className={styles.assigneeAvatar}>
        {initial}
      </span>
      <span>
        <strong>{assignee.displayName || assignee.username}</strong>
        <small>@{assignee.username}</small>
      </span>
    </span>
  );
}

function mergeAssignees(current: Assignee[], candidates: Assignee[]): Assignee[] {
  const users = new Map<string, Assignee>();
  for (const user of [...current, ...candidates]) users.set(user.id, user);
  return [...users.values()];
}

function LabelChip({ label }: { label: Label }) {
  return (
    <span className={styles.label} style={{ "--label-color": safeColor(label.color) } as CSSProperties}>
      {label.name}
    </span>
  );
}

function safeColor(value: string): string {
  return /^#[0-9a-f]{6}$/i.test(value) ? value : /^([0-9a-f]{6})$/i.test(value) ? `#${value}` : "#6e7781";
}
