import { Check, CircleDot, MessageSquare } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { Issue } from "@/lib/api-types";
import { formatRelativeTime, labelTextColor, normalizeLabelColor } from "@/lib/format";
import { repositoryPath } from "@/lib/routes";

import { UserAvatar } from "../ui/user-avatar";
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

  const copy = dictionary.workItemLists;
  return (
    <div className={styles.list}>
      {issues.map((issue) => (
        <article className={styles.row} key={issue.id}>
          {issue.state === "closed" ? (
            <Check aria-hidden="true" className={styles.stateIcon} data-state={issue.state} size={18} />
          ) : (
            <CircleDot aria-hidden="true" className={styles.stateIcon} data-state={issue.state} size={18} />
          )}
          <div className={styles.content}>
            <h3>
              <Link href={`${repositoryPath(locale, owner, repository, "issues")}/${issue.number}`}>{issue.title}</Link>
              {issue.labels.map((label) => {
                const bg = normalizeLabelColor(label.color);
                return (
                  <span
                    className={styles.label}
                    key={label.id}
                    style={{ backgroundColor: bg, color: labelTextColor(bg) }}
                  >
                    {label.name}
                  </span>
                );
              })}
            </h3>
            <p>
              {copy.openedBy
                .replace("{number}", String(issue.number))
                .replace("{time}", formatRelativeTime(issue.createdAt, locale))
                .replace("{author}", issue.author)}
              {issue.milestone && (
                <>
                  {" · "}
                  <Link
                    href={`${repositoryPath(locale, owner, repository, "issues")}?milestone=${issue.milestone.number}`}
                  >
                    {issue.milestone.title}
                  </Link>
                </>
              )}
            </p>
          </div>
          <div className={styles.trailing}>
            {issue.assignees.length > 0 && (
              <div aria-label={dictionary.issueAssignees.title} className={styles.assignees}>
                {issue.assignees.slice(0, 3).map((assignee) => (
                  <Link
                    aria-label={dictionary.issueAssignees.assignedTo.replace("{username}", assignee.username)}
                    href={`${repositoryPath(locale, owner, repository, "issues")}?assignee=${assignee.username}`}
                    key={assignee.id}
                    title={`@${assignee.username}`}
                  >
                    <UserAvatar
                      avatarUrl={assignee.avatarUrl}
                      name={assignee.displayName || assignee.username}
                      size={20}
                    />
                  </Link>
                ))}
              </div>
            )}
            {issue.commentCount > 0 && (
              <span className={styles.comments}>
                <MessageSquare aria-hidden="true" size={14} />
                {issue.commentCount}
              </span>
            )}
          </div>
        </article>
      ))}
    </div>
  );
}
