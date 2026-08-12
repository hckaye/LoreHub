import { GitMerge, GitPullRequest, MessageSquare } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { MergeRequestListItem } from "@/lib/api-types";
import { repositoryMilestonesPath, repositoryPath } from "@/lib/routes";

import { EmptyState } from "../ui/empty-state";
import styles from "./merge-request-list.module.css";

type MergeRequestListProps = {
  mergeRequests: MergeRequestListItem[];
  dictionary: Dictionary;
  locale: Locale;
  owner: string;
  repository: string;
};

export function MergeRequestList({ mergeRequests, dictionary, locale, owner, repository }: MergeRequestListProps) {
  if (mergeRequests.length === 0) {
    return (
      <EmptyState
        icon={<GitPullRequest aria-hidden="true" />}
        title={dictionary.repository.noPullRequests}
        body={dictionary.repository.pullRequestsDescription}
      />
    );
  }

  return (
    <div className={styles.list}>
      {mergeRequests.map((mergeRequest) => (
        <article className={styles.row} key={mergeRequest.id}>
          <GitPullRequest aria-hidden="true" className={styles.icon} data-state={mergeRequest.state} size={18} />
          <div className={styles.details}>
            <h3>
              <Link href={`${repositoryPath(locale, owner, repository, "pulls")}/${mergeRequest.number}`}>
                {mergeRequest.title}
              </Link>
              {mergeRequest.isDraft && <span className={styles.draft}>{dictionary.pullRequestDrafts.badge}</span>}
              {mergeRequest.labels.map((label) => (
                <span className={styles.label} key={label.id} style={{ borderColor: `#${label.color}` }}>
                  {label.name}
                </span>
              ))}
            </h3>
            <p>
              #{mergeRequest.number} · {mergeRequest.author} · {formatDate(mergeRequest.updatedAt, locale)}
              {mergeRequest.milestone && (
                <>
                  {" · "}
                  <Link href={repositoryMilestonesPath(locale, owner, repository)}>{mergeRequest.milestone.title}</Link>
                </>
              )}
            </p>
          </div>
          <div className={styles.branches}>
            <code>{mergeRequest.sourceBranch}</code>
            <GitMerge aria-hidden="true" size={14} />
            <code>{mergeRequest.targetBranch}</code>
          </div>
          <div className={styles.trailing}>
            {mergeRequest.assignees.length > 0 && (
              <span
                aria-label={`${dictionary.issueAssignees.title}: ${mergeRequest.assignees
                  .map((assignee) => `@${assignee.username}`)
                  .join(", ")}`}
              >
                {mergeRequest.assignees.length}
              </span>
            )}
            <span>
              {mergeRequest.approvalCount} {dictionary.repository.approvals}
            </span>
            {mergeRequest.commentCount > 0 && (
              <span>
                <MessageSquare aria-hidden="true" size={14} />
                {mergeRequest.commentCount}
              </span>
            )}
          </div>
        </article>
      ))}
    </div>
  );
}

function formatDate(value: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(new Date(value));
}
