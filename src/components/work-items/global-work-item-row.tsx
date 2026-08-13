import {
  Check,
  CircleDot,
  GitMerge,
  GitPullRequest,
  GitPullRequestClosed,
  MessageSquare,
  ThumbsUp,
} from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { GlobalWorkItem } from "@/lib/global-work-items";
import { formatRelativeTime, labelTextColor, normalizeLabelColor } from "@/lib/format";
import { repositoryPath } from "@/lib/routes";

import { UserAvatar } from "../ui/user-avatar";
import styles from "./global-work-item-list.module.css";

type GlobalWorkItemRowProps = {
  dictionary: Dictionary;
  item: GlobalWorkItem;
  locale: Locale;
};

export function GlobalWorkItemRow({ dictionary, item, locale }: GlobalWorkItemRowProps) {
  const section = item.kind === "issue" ? "issues" : "pulls";
  const itemHref = `${repositoryPath(locale, item.repository.owner, item.repository.slug, section)}/${item.number}`;
  const repoHref = repositoryPath(locale, item.repository.owner, item.repository.slug);
  const repository = `${item.repository.owner}/${item.repository.slug}`;
  const metadata = dictionary.globalWorkItems.itemMetadata
    .replace("{number}", String(item.number))
    .replace("{time}", formatRelativeTime(item.createdAt, locale))
    .replace("{author}", item.author.username);
  return (
    <article className={styles.row} data-state={item.state}>
      <StateIcon isDraft={item.isDraft} kind={item.kind} state={item.state} />
      <div className={styles.content}>
        <h2>
          <Link href={itemHref}>{item.title}</Link>
          {item.isDraft && <span className={styles.draft}>{dictionary.globalWorkItems.draft}</span>}
          {item.labels.map((label) => {
            const bg = normalizeLabelColor(label.color);
            return (
              <span
                className={styles.labelChip}
                key={label.id}
                style={{ backgroundColor: bg, color: labelTextColor(bg) }}
              >
                {label.name}
              </span>
            );
          })}
        </h2>
        <p>
          <Link href={repoHref}>{repository}</Link> {metadata}
        </p>
        {item.kind === "pull_request" && (
          <div className={styles.branches}>
            <code>{item.sourceBranch}</code>
            <GitMerge aria-hidden="true" size={13} />
            <code>{item.targetBranch}</code>
          </div>
        )}
      </div>
      <div className={styles.trailing}>
        {item.assignees.length > 0 && (
          <div className={styles.assignees}>
            {item.assignees.slice(0, 3).map((assignee) => (
              <Link
                href={repositoryPath(locale, item.repository.owner, item.repository.slug, section)}
                key={assignee.id}
                title={`@${assignee.username}`}
              >
                <UserAvatar avatarUrl={assignee.avatarUrl} name={assignee.displayName || assignee.username} size={20} />
              </Link>
            ))}
          </div>
        )}
        {item.commentCount > 0 && (
          <span title={dictionary.globalWorkItems.comments.replace("{count}", String(item.commentCount))}>
            <MessageSquare aria-hidden="true" size={14} /> {item.commentCount}
          </span>
        )}
        {item.kind === "pull_request" && item.approvalCount > 0 && (
          <span title={dictionary.globalWorkItems.approvals.replace("{count}", String(item.approvalCount))}>
            <ThumbsUp aria-hidden="true" size={14} /> {item.approvalCount}
          </span>
        )}
      </div>
    </article>
  );
}

function StateIcon(props: { isDraft: boolean; kind: GlobalWorkItem["kind"]; state: GlobalWorkItem["state"] }) {
  if (props.kind === "issue") {
    return props.state === "closed" ? (
      <Check aria-hidden="true" className={styles.stateIcon} data-state={props.state} size={18} />
    ) : (
      <CircleDot aria-hidden="true" className={styles.stateIcon} data-state={props.state} size={18} />
    );
  }
  if (props.isDraft && props.state === "open") {
    return <GitPullRequest aria-hidden="true" className={styles.stateIcon} data-state="draft" size={18} />;
  }
  if (props.state === "merged") {
    return <GitMerge aria-hidden="true" className={styles.stateIcon} data-state={props.state} size={18} />;
  }
  if (props.state === "closed") {
    return <GitPullRequestClosed aria-hidden="true" className={styles.stateIcon} data-state={props.state} size={18} />;
  }
  return <GitPullRequest aria-hidden="true" className={styles.stateIcon} data-state={props.state} size={18} />;
}
