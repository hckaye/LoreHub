"use client";

import { useEffect, useState, type CSSProperties } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { Assignee, Issue, Label, Milestone } from "@/lib/api-types";
import { formatDate, labelTextColor, normalizeLabelColor } from "@/lib/format";
import { searchIssueAssignees } from "@/lib/issue-assignee-client";

import { UserAvatar } from "../ui/user-avatar";
import styles from "./issue-detail.module.css";
import { SidebarMenu } from "./issue-sidebar-menu";

type IssueSidebarProps = {
  busyAction: string | null;
  dictionary: Dictionary;
  issue: Issue;
  labels: Label[];
  labelsAvailable: boolean;
  locale: Locale;
  milestones: Milestone[];
  milestonesAvailable: boolean;
  owner: string;
  repository: string;
  assignableUsers: Assignee[];
  assigneesAvailable: boolean;
  onSetAssignee: (assignee: Assignee, selected: boolean) => Promise<void>;
  onSetMilestone: (number: number | null) => Promise<void>;
  onToggleLabel: (label: Label, selected: boolean) => Promise<void>;
};

export function IssueSidebar(props: IssueSidebarProps) {
  return (
    <aside className={styles.sidebar}>
      <AssigneeSection {...props} />
      <LabelSection {...props} />
      <MilestoneSection {...props} />
    </aside>
  );
}

function LabelSection(props: IssueSidebarProps) {
  const copy = props.dictionary.issueDetail;
  const selected = new Set(props.issue.labels.map((label) => label.id));
  return (
    <section>
      <div className={styles.sidebarHeading}>
        <h2>{copy.labels}</h2>
        {props.issue.viewerCanManageLabels && props.labelsAvailable && (
          <SidebarMenu menuClassName={styles.labelMenu} summary={copy.manageLabels}>
            {(close) =>
              props.labels.map((label) => (
                <label key={label.id}>
                  <input
                    checked={selected.has(label.id)}
                    disabled={props.busyAction === label.id}
                    onChange={(event) => {
                      close();
                      props.onToggleLabel(label, event.target.checked);
                    }}
                    type="checkbox"
                  />
                  <LabelChip label={label} />
                </label>
              ))
            }
          </SidebarMenu>
        )}
      </div>
      {props.issue.labels.length > 0 ? (
        <div className={styles.labels}>
          {props.issue.labels.map((label) => (
            <LabelChip key={label.id} label={label} />
          ))}
        </div>
      ) : (
        <p>{copy.noLabels}</p>
      )}
      {!props.labelsAvailable && <p>{copy.labelsUnavailable}</p>}
    </section>
  );
}

function MilestoneSection(props: IssueSidebarProps) {
  const copy = props.dictionary.issueDetail;
  const milestone = props.issue.milestone;
  return (
    <section>
      <div className={styles.sidebarHeading}>
        <h2>{copy.milestone}</h2>
        {props.issue.viewerCanManageMilestone && props.milestonesAvailable && (
          <SidebarMenu menuClassName={styles.milestoneMenu} summary={copy.manageMilestone}>
            {(close) => (
              <>
                <button
                  disabled={props.busyAction === "milestone"}
                  onClick={() => {
                    close();
                    props.onSetMilestone(null);
                  }}
                  type="button"
                >
                  {copy.clearMilestone}
                </button>
                {props.milestones.map((option) => (
                  <button
                    aria-pressed={milestone?.id === option.id}
                    disabled={props.busyAction === "milestone"}
                    key={option.id}
                    onClick={() => {
                      close();
                      props.onSetMilestone(option.number);
                    }}
                    type="button"
                  >
                    <span>{option.title}</span>
                    <small>
                      {option.state === "open" ? props.dictionary.common.open : props.dictionary.common.closed}
                    </small>
                  </button>
                ))}
              </>
            )}
          </SidebarMenu>
        )}
      </div>
      {milestone ? (
        <div className={styles.milestoneValue}>
          <strong>{milestone.title}</strong>
          <span>
            {milestone.dueOn
              ? copy.milestoneDue.replace("{date}", formatDate(milestone.dueOn, props.locale))
              : copy.milestoneNoDueDate}
          </span>
        </div>
      ) : (
        <p>{copy.noMilestone}</p>
      )}
      {!props.milestonesAvailable && <p>{copy.milestonesUnavailable}</p>}
    </section>
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
          <SidebarMenu menuClassName={styles.assigneeMenu} summary={props.dictionary.issueAssignees.manage}>
            {(close) => (
              <>
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
                        onChange={(event) => {
                          close();
                          props.onSetAssignee(assignee, event.target.checked);
                        }}
                        type="checkbox"
                      />
                      <AssigneeIdentity assignee={assignee} />
                    </label>
                  ))
                ) : (
                  <p>{props.dictionary.issueAssignees.noCandidates}</p>
                )}
              </>
            )}
          </SidebarMenu>
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
  const name = assignee.displayName || assignee.username;
  return (
    <span className={styles.assigneeIdentity}>
      <UserAvatar avatarUrl={assignee.avatarUrl} name={name} size={26} />
      <span>
        <strong>{name}</strong>
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
  const background = normalizeLabelColor(label.color);
  const style = { "--label-color": background, "--label-fg": labelTextColor(label.color) } as CSSProperties;
  return (
    <span className={styles.label} style={style}>
      {label.name}
    </span>
  );
}
