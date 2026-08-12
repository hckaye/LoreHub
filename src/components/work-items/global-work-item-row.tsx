import { CircleDot, GitMerge, GitPullRequest, MessageSquare, ThumbsUp } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { GlobalWorkItem } from "@/lib/global-work-items";
import { repositoryPath } from "@/lib/routes";

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
    .replace("{author}", item.author.username)
    .replace("{date}", formatDate(item.updatedAt, locale));
  return (
    <article className={styles.row} data-state={item.state}>
      {item.kind === "issue" ? (
        <CircleDot aria-hidden="true" className={styles.stateIcon} size={18} />
      ) : (
        <GitPullRequest aria-hidden="true" className={styles.stateIcon} size={18} />
      )}
      <div className={styles.content}>
        <h2>
          <Link href={itemHref}>{item.title}</Link>
          {item.isDraft && <span className={styles.draft}>{dictionary.globalWorkItems.draft}</span>}
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
        {item.labels.length > 0 && (
          <div className={styles.labels}>
            {item.labels.map((label) => (
              <span key={label.id} style={{ borderColor: `#${label.color}` }}>
                {label.name}
              </span>
            ))}
          </div>
        )}
      </div>
      <div className={styles.trailing}>
        {item.assignees.length > 0 && (
          <div className={styles.assignees}>
            {item.assignees.slice(0, 3).map((assignee) => (
              <span key={assignee.id} title={`@${assignee.username}`}>
                {[...(assignee.displayName || assignee.username)][0]?.toUpperCase() ?? "?"}
              </span>
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

function formatDate(value: string, locale: Locale): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(new Date(value));
}
