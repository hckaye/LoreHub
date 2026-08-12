import Link from "next/link";

import type { Dictionary } from "@/i18n";
import { lastRevisionCommentPage } from "@/lib/revision-comments";

import styles from "./revision-comments.module.css";

type RevisionCommentPaginationProps = {
  dictionary: Dictionary;
  hasNext: boolean;
  href: (page: number) => string;
  page: number;
  perPage: number;
  totalCount: number;
};

export function RevisionCommentPagination(props: RevisionCommentPaginationProps) {
  const pages = lastRevisionCommentPage(props.totalCount, props.perPage);
  if (pages === 1 && props.page === 1) return null;
  const copy = props.dictionary.commentPagination;
  return (
    <nav aria-label={copy.label} className={styles.pagination}>
      {props.page > 1 ? <Link href={props.href(props.page - 1)}>{copy.previous}</Link> : <span>{copy.previous}</span>}
      <strong>
        {props.page} / {pages}
      </strong>
      {props.hasNext ? <Link href={props.href(props.page + 1)}>{copy.next}</Link> : <span>{copy.next}</span>}
    </nav>
  );
}
