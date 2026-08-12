import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { DiscussionQuery } from "@/lib/lorehub-api";

import styles from "./discussion-list.module.css";

type DiscussionListPaginationProps = {
  basePath: string;
  dictionary: Dictionary;
  page: number;
  pages: number;
  query: DiscussionQuery;
};

export function DiscussionListPagination(props: DiscussionListPaginationProps) {
  function href(nextPage: number) {
    const params = new URLSearchParams();
    if (props.query.q) params.set("q", props.query.q);
    if (props.query.category) params.set("category", props.query.category);
    if (props.query.state) params.set("state", props.query.state);
    if (props.query.sort) params.set("sort", props.query.sort);
    params.set("page", String(nextPage));
    return `${props.basePath}?${params}`;
  }
  return (
    <nav aria-label={props.dictionary.discussionsPage.filterLabel} className={styles.pagination}>
      {props.page > 1 ? <Link href={href(props.page - 1)}>{props.dictionary.discussionsPage.previous}</Link> : <span />}
      <span>
        {props.dictionary.discussionsPage.pageOf
          .replace("{page}", String(props.page))
          .replace("{pages}", String(props.pages))}
      </span>
      {props.page < props.pages ? (
        <Link href={href(props.page + 1)}>{props.dictionary.discussionsPage.next}</Link>
      ) : (
        <span />
      )}
    </nav>
  );
}
