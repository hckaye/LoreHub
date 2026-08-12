import Link from "next/link";

import type { Locale } from "@/i18n/config";
import { repositoryPath } from "@/lib/routes";

import type { DiscussionCopy } from "./discussion-component-types";
import styles from "./discussion-detail.module.css";

type DiscussionCommentPaginationProps = {
  commentPage: number;
  commentsPerPage: number;
  copy: DiscussionCopy;
  discussionNumber: number;
  locale: Locale;
  owner: string;
  repository: string;
  totalComments: number;
};

export function DiscussionCommentPagination(props: DiscussionCommentPaginationProps) {
  const pages = Math.max(1, Math.ceil(props.totalComments / Math.max(1, props.commentsPerPage)));
  if (pages <= 1) return null;
  const discussionPath = repositoryPath(props.locale, props.owner, props.repository, "discussions");
  const basePath = `${discussionPath}/${props.discussionNumber}`;
  const href = (page: number) => `${basePath}?comment_page=${page}`;
  return (
    <nav aria-label={props.copy.comments} className={styles.pagination}>
      {props.commentPage > 1 ? <Link href={href(props.commentPage - 1)}>{props.copy.previous}</Link> : <span />}
      <span>{props.copy.pageOf.replace("{page}", String(props.commentPage)).replace("{pages}", String(pages))}</span>
      {props.commentPage < pages ? <Link href={href(props.commentPage + 1)}>{props.copy.next}</Link> : <span />}
    </nav>
  );
}
