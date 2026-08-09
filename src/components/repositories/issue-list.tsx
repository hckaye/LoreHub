import { CircleDot } from "lucide-react";

import type { Dictionary } from "@/i18n";
import type { Issue } from "@/lib/api-types";

import { EmptyState } from "../ui/empty-state";
import styles from "./issue-list.module.css";

type IssueListProps = {
  issues: Issue[];
  dictionary: Dictionary;
  locale: string;
};

export function IssueList({ issues, dictionary, locale }: IssueListProps) {
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
          <div>
            <h3>{issue.title}</h3>
            <p>
              #{issue.number} · {issue.author} · {formatDate(issue.updatedAt, locale)}
            </p>
          </div>
          {issue.commentCount > 0 && <span className={styles.comments}>{issue.commentCount}</span>}
        </article>
      ))}
    </div>
  );
}

function formatDate(value: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(new Date(value));
}
