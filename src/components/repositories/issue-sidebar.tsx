import type { CSSProperties } from "react";

import type { Dictionary } from "@/i18n";
import type { Issue, Label } from "@/lib/api-types";

import styles from "./issue-detail.module.css";

type IssueSidebarProps = {
  busyAction: string | null;
  dictionary: Dictionary;
  issue: Issue;
  labels: Label[];
  labelsAvailable: boolean;
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
