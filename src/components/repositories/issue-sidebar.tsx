import type { CSSProperties } from "react";

import type { Dictionary } from "@/i18n";
import type { Issue, Label, Milestone } from "@/lib/api-types";

import styles from "./issue-detail.module.css";

type IssueSidebarProps = {
  busyAction: string | null;
  dictionary: Dictionary;
  issue: Issue;
  labels: Label[];
  labelsAvailable: boolean;
  milestones: Milestone[];
  milestonesAvailable: boolean;
  onSetMilestone: (number: number | null) => Promise<void>;
  onToggleLabel: (label: Label, selected: boolean) => Promise<void>;
  onUpdateState: (state: "open" | "closed") => Promise<boolean>;
};

export function IssueSidebar(props: IssueSidebarProps) {
  const selected = new Set(props.issue.labels.map((label) => label.id));
  return (
    <aside className={styles.sidebar}>
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
