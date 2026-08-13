import { GitMerge, GitPullRequest, GitPullRequestClosed, MessageSquare } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { MergeRequestListItem } from "@/lib/api-types";
import { formatRelativeTime, labelTextColor, normalizeLabelColor } from "@/lib/format";
import { repositoryPath } from "@/lib/routes";

import { UserAvatar } from "../ui/user-avatar";
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
        body={dictionary.pullRequestsPage.emptyBody}
        icon={<GitPullRequest aria-hidden="true" />}
        title={dictionary.pullRequestsPage.emptyTitle}
      />
    );
  }

  const copy = dictionary.workItemLists;
  return (
    <div className={styles.list}>
      {mergeRequests.map((mergeRequest) => (
        <article className={styles.row} key={mergeRequest.id}>
          <StateIcon isDraft={mergeRequest.isDraft} state={mergeRequest.state} />
          <div className={styles.details}>
            <h3>
              <Link href={`${repositoryPath(locale, owner, repository, "pulls")}/${mergeRequest.number}`}>
                {mergeRequest.title}
              </Link>
              {mergeRequest.isDraft && <span className={styles.draft}>{dictionary.pullRequestDrafts.badge}</span>}
              {mergeRequest.labels.map((label) => {
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
                .replace("{number}", String(mergeRequest.number))
                .replace("{time}", formatRelativeTime(mergeRequest.createdAt, locale))
                .replace("{author}", mergeRequest.author)}
              {mergeRequest.milestone && (
                <>
                  {" · "}
                  <Link
                    href={`${repositoryPath(locale, owner, repository, "pulls")}?milestone=${
                      mergeRequest.milestone.number
                    }`}
                  >
                    {mergeRequest.milestone.title}
                  </Link>
                </>
              )}
            </p>
          </div>
          <div className={styles.trailing}>
            {mergeRequest.assignees.length > 0 && (
              <div
                aria-label={dictionary.issueAssignees.title}
                className={styles.assignees}
              >
                {mergeRequest.assignees.slice(0, 3).map((assignee) => (
                  <Link
                    aria-label={dictionary.issueAssignees.assignedTo.replace("{username}", assignee.username)}
                    href={`${repositoryPath(locale, owner, repository, "pulls")}?assignee=${assignee.username}`}
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
            {mergeRequest.commentCount > 0 && (
              <span className={styles.comments}>
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

function StateIcon(props: { isDraft: boolean; state: MergeRequestListItem["state"] }) {
  if (props.isDraft && props.state === "open") {
    return <GitPullRequest aria-hidden="true" className={styles.icon} data-state="draft" size={18} />;
  }
  if (props.state === "merged") {
    return <GitMerge aria-hidden="true" className={styles.icon} data-state={props.state} size={18} />;
  }
  if (props.state === "closed") {
    return <GitPullRequestClosed aria-hidden="true" className={styles.icon} data-state={props.state} size={18} />;
  }
  return <GitPullRequest aria-hidden="true" className={styles.icon} data-state={props.state} size={18} />;
}
