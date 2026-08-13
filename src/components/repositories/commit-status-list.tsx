import { CircleAlert, CircleCheck, CircleDot, CircleX, ListChecks } from "lucide-react";
import type { ReactNode } from "react";

import type { Dictionary } from "@/i18n";
import type { MergeStatusCheck, RevisionStatus, RevisionStatusState } from "@/lib/api-types";
import { formatDateTime } from "@/lib/format";

import styles from "./commit-status-list.module.css";

type CommitStatusListProps = {
  dictionary: Dictionary;
  locale: "en" | "ja";
  statuses: Array<RevisionStatus | MergeStatusCheck> | null;
  state?: RevisionStatusState;
  unavailableReason?: "forbidden" | "unavailable";
};

export function CommitStatusList(props: CommitStatusListProps) {
  const copy = props.dictionary.commitStatuses;
  if (props.statuses === null) {
    const forbidden = props.unavailableReason === "forbidden";
    return (
      <section className={styles.panel} data-tone="unavailable">
        <div className={styles.emptyIcon}>
          <CircleAlert aria-hidden="true" />
        </div>
        <div>
          <h2>{forbidden ? copy.forbiddenTitle : copy.unavailableTitle}</h2>
          <p>{forbidden ? copy.forbiddenBody : copy.unavailableBody}</p>
        </div>
      </section>
    );
  }
  const state = props.state ?? combinedState(props.statuses);
  return (
    <section aria-labelledby="commit-status-title" className={styles.panel}>
      <header className={styles.heading} data-state={state}>
        {stateIcon(state)}
        <div>
          <h2 id="commit-status-title">{copy.title}</h2>
          <p>{copy.summary.replace("{count}", String(props.statuses.length))}</p>
        </div>
      </header>
      {props.statuses.length === 0 ? (
        <div className={styles.empty}>
          <ListChecks aria-hidden="true" />
          <div>
            <h3>{copy.emptyTitle}</h3>
            <p>{copy.emptyBody}</p>
          </div>
        </div>
      ) : (
        <ul className={styles.list}>
          {props.statuses.map((status) => (
            <li key={statusKey(status)}>
              <span className={styles.state} data-state={status.state}>
                {stateIcon(status.state)}
                <span>{copy.states[status.state]}</span>
              </span>
              <div className={styles.details}>
                <div className={styles.contextLine}>
                  <strong>{status.context}</strong>
                  {isMergeStatus(status) && status.required && <span className={styles.required}>{copy.required}</span>}
                </div>
                {status.description && <p>{status.description}</p>}
                <small>
                  {copy.reportedBy
                    .replace("{creator}", statusCreator(status))
                    .replace("{date}", formatDateTime(statusTimestamp(status), props.locale))}
                </small>
              </div>
              {status.targetUrl && (
                <a href={status.targetUrl} rel="noreferrer" target="_blank">
                  {copy.details}
                </a>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function combinedState(statuses: Array<RevisionStatus | MergeStatusCheck>): RevisionStatusState {
  if (statuses.some((status) => status.state === "error")) return "error";
  if (statuses.some((status) => status.state === "failure")) return "failure";
  if (statuses.length === 0 || statuses.some((status) => status.state === "pending")) return "pending";
  return "success";
}

function stateIcon(state: RevisionStatusState): ReactNode {
  if (state === "success") return <CircleCheck aria-hidden="true" />;
  if (state === "failure") return <CircleX aria-hidden="true" />;
  if (state === "error") return <CircleAlert aria-hidden="true" />;
  return <CircleDot aria-hidden="true" />;
}

function isMergeStatus(status: RevisionStatus | MergeStatusCheck): status is MergeStatusCheck {
  return "required" in status;
}

function statusCreator(status: RevisionStatus | MergeStatusCheck): string {
  if (isMergeStatus(status)) return status.creator;
  return status.creator.displayName
    ? `${status.creator.displayName} (@${status.creator.username})`
    : `@${status.creator.username}`;
}

function statusTimestamp(status: RevisionStatus | MergeStatusCheck): string {
  return isMergeStatus(status) ? status.updatedAt : status.createdAt;
}

function statusKey(status: RevisionStatus | MergeStatusCheck): string {
  return isMergeStatus(status) ? `${status.context}-${status.updatedAt}` : status.id;
}
