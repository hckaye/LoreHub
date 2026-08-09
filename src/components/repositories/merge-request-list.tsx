import { GitMerge, GitPullRequest } from "lucide-react";

import type { Dictionary } from "@/i18n";
import type { MergeRequest } from "@/lib/api-types";

import { EmptyState } from "../ui/empty-state";
import styles from "./merge-request-list.module.css";

type MergeRequestListProps = {
  mergeRequests: MergeRequest[];
  dictionary: Dictionary;
};

export function MergeRequestList({ mergeRequests, dictionary }: MergeRequestListProps) {
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
          <GitPullRequest aria-hidden="true" className={styles.icon} size={18} />
          <div className={styles.details}>
            <h3>{mergeRequest.title}</h3>
            <p>
              #{mergeRequest.number} · {mergeRequest.author}
            </p>
          </div>
          <div className={styles.branches}>
            <code>{mergeRequest.sourceBranch}</code>
            <GitMerge aria-hidden="true" size={14} />
            <code>{mergeRequest.targetBranch}</code>
          </div>
          <span className={styles.approvals}>
            {mergeRequest.approvalCount} {dictionary.repository.approvals}
          </span>
        </article>
      ))}
    </div>
  );
}
