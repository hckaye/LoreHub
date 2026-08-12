import Link from "next/link";

import type { Dictionary } from "@/i18n";
import { conversationCommentPageHref, lastConversationCommentPage } from "@/lib/comment-pagination";

import styles from "./conversation-pagination.module.css";

type ConversationPaginationProps = {
  basePath: string;
  dictionary: Dictionary;
  hasNext: boolean;
  page: number;
  perPage: number;
  totalCount: number;
};

export function ConversationPagination(props: ConversationPaginationProps) {
  const pages = lastConversationCommentPage(props.totalCount, props.perPage);
  if (pages === 1 && props.page === 1) return null;
  const copy = props.dictionary.commentPagination;
  return (
    <nav aria-label={copy.label} className={styles.pagination}>
      {props.page > 1 ? (
        <Link href={conversationCommentPageHref(props.basePath, props.page - 1)}>{copy.previous}</Link>
      ) : (
        <span aria-disabled="true">{copy.previous}</span>
      )}
      <strong>{copy.page.replace("{page}", String(props.page)).replace("{pages}", String(pages))}</strong>
      {props.hasNext ? (
        <Link href={conversationCommentPageHref(props.basePath, props.page + 1)}>{copy.next}</Link>
      ) : (
        <span aria-disabled="true">{copy.next}</span>
      )}
    </nav>
  );
}
