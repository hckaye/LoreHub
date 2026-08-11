import { CircleDot } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { Issue } from "@/lib/api-types";
import { repositoryMilestonesPath, repositoryPath } from "@/lib/routes";

import { EmptyState } from "../ui/empty-state";
import styles from "./issue-list.module.css";

type IssueListProps = {
  issues: Issue[];
  dictionary: Dictionary;
  locale: Locale;
  owner: string;
  repository: string;
};

export function IssueList({ issues, dictionary, locale, owner, repository }: IssueListProps) {
  if (issues.length === 0) {
    return (
      <EmptyState
        body={dictionary.issuesPage.emptyBody}
        icon={<CircleDot aria-hidden="true" />}
        title={dictionary.issuesPage.emptyTitle}
      />
    );
  }

  return (
    <div className={styles.list}>
      {issues.map((issue) => (
        <article className={styles.row} key={issue.id}>
          <CircleDot aria-hidden="true" className={styles.stateIcon} size={18} />
          <div className={styles.content}>
            <h3>
              <Link href={`${repositoryPath(locale, owner, repository, "issues")}/${issue.number}`}>{issue.title}</Link>
            </h3>
            <p>
              #{issue.number} · {issue.author} · {formatDate(issue.updatedAt, locale)}
              {issue.milestone && (
                <>
                  {" · "}
                  <Link href={repositoryMilestonesPath(locale, owner, repository)}>{issue.milestone.title}</Link>
                </>
              )}
            </p>
          </div>
          <div className={styles.trailing}>
            {issue.assignees.length > 0 && (
              <div aria-label={dictionary.issueAssignees.title} className={styles.assignees}>
                {issue.assignees.slice(0, 3).map((assignee) => (
                  <span
                    aria-label={dictionary.issueAssignees.assignedTo.replace("{username}", assignee.username)}
                    key={assignee.id}
                    title={`@${assignee.username}`}
                  >
                    {[...(assignee.displayName || assignee.username)][0]?.toUpperCase() ?? "?"}
                  </span>
                ))}
              </div>
            )}
            {issue.commentCount > 0 && <span className={styles.comments}>{issue.commentCount}</span>}
          </div>
        </article>
      ))}
    </div>
  );
}

function formatDate(value: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(new Date(value));
}
