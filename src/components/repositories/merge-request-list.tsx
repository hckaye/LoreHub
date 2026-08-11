import { GitMerge, GitPullRequest } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { MergeRequest } from "@/lib/api-types";
import { repositoryPath } from "@/lib/routes";

import { EmptyState } from "../ui/empty-state";
import styles from "./merge-request-list.module.css";

type MergeRequestListProps = {
  mergeRequests: MergeRequest[];
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
          <GitPullRequest aria-hidden="true" className={styles.icon} size={18} />
          <div className={styles.details}>
            <h3>
              <Link href={`${repositoryPath(locale, owner, repository, "pulls")}/${mergeRequest.number}`}>
                {mergeRequest.title}
              </Link>
              {mergeRequest.isDraft && <span className={styles.draft}>{dictionary.pullRequestDrafts.badge}</span>}
            </h3>
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
