import { CalendarDays, Pencil } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { Issue, Milestone } from "@/lib/api-types";
import { formatDate } from "@/lib/format";
import { repositoryMilestonePath, repositoryMilestonesPath } from "@/lib/routes";

import { FilterTabs } from "../ui/filter-tabs";
import { IssueList } from "./issue-list";
import styles from "./milestone-detail.module.css";

type MilestoneDetailProps = {
  milestone: Milestone;
  issues: Issue[];
  openCount: number;
  closedCount: number;
  state: "open" | "closed";
  dictionary: Dictionary;
  locale: Locale;
  owner: string;
  repository: string;
};

export function MilestoneDetail(props: MilestoneDetailProps) {
  const labels = props.dictionary.milestonesPage;
  const total = props.milestone.openIssueCount + props.milestone.closedIssueCount;
  const percent = total === 0 ? 0 : Math.round((props.milestone.closedIssueCount / total) * 100);
  const basePath = repositoryMilestonePath(props.locale, props.owner, props.repository, props.milestone.number);
  const editHref = repositoryMilestonesPath(props.locale, props.owner, props.repository);

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div className={styles.headerMain}>
          <h1>{props.milestone.title}</h1>
          <p className={styles.meta}>
            {labels.detailCreatedBy.replace("{author}", props.milestone.createdBy)}
            {props.milestone.dueOn && (
              <>
                {" · "}
                <CalendarDays aria-hidden="true" size={14} />
                {labels.detailDueOn.replace("{date}", formatDate(`${props.milestone.dueOn}T00:00:00Z`, props.locale))}
              </>
            )}
          </p>
          {props.milestone.description ? (
            <p className={styles.description}>{props.milestone.description}</p>
          ) : (
            <p className={styles.description + " " + styles.muted}>{labels.noDescription}</p>
          )}
        </div>
        <Link className={styles.editLink} href={editHref}>
          <Pencil aria-hidden="true" size={15} />
          {labels.detailEdit}
        </Link>
      </header>

      <div className={styles.progress}>
        <div
          aria-label={labels.progress.replace("{percent}", String(percent))}
          aria-valuemax={100}
          aria-valuemin={0}
          aria-valuenow={percent}
          role="progressbar"
        >
          <span style={{ width: `${percent}%` }} />
        </div>
        <p>
          <strong>{labels.progress.replace("{percent}", String(percent))}</strong>
          <span>{labels.openIssues.replace("{count}", String(props.milestone.openIssueCount))}</span>
          <span>{labels.closedIssues.replace("{count}", String(props.milestone.closedIssueCount))}</span>
        </p>
      </div>

      <FilterTabs
        label={labels.detailIssuesLabel}
        tabs={[
          {
            active: props.state === "open",
            count: props.openCount,
            href: basePath,
            label: labels.detailOpenTab,
          },
          {
            active: props.state === "closed",
            count: props.closedCount,
            href: `${basePath}?state=closed`,
            label: labels.detailClosedTab,
          },
        ]}
      />

      <IssueList
        dictionary={props.dictionary}
        issues={props.issues}
        locale={props.locale}
        owner={props.owner}
        repository={props.repository}
      />
    </div>
  );
}
