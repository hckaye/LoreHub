import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { RepositoryIssueQuery, RepositoryMergeRequestQuery } from "@/lib/repository-work-item-query";
import { repositoryWorkItemSearchParams } from "@/lib/repository-work-item-query";

import styles from "./work-item-list-toolbar.module.css";

type WorkItemListPaginationProps = {
  basePath: string;
  dictionary: Dictionary;
  hasNext: boolean;
  page: number;
  perPage?: number;
  totalCount?: number;
  query: RepositoryIssueQuery | RepositoryMergeRequestQuery;
};

export function WorkItemListPagination(props: WorkItemListPaginationProps) {
  const copy = props.dictionary.workItemLists;
  const perPage = props.perPage ?? 25;
  const total = props.totalCount ?? 0;
  const totalPages = total > 0 ? Math.max(1, Math.ceil(total / perPage)) : props.hasNext ? props.page + 1 : props.page;
  const pages = pageRange(props.page, totalPages);
  const prevDisabled = props.page <= 1;
  const nextDisabled = !props.hasNext;
  return (
    <nav aria-label={copy.page.replace("{page}", String(props.page))} className={styles.pagination}>
      {prevDisabled ? (
        <button className={styles.paginationButton} disabled type="button">
          {copy.previous}
        </button>
      ) : (
        <Link className={styles.paginationButton} href={pageHref(props.basePath, props.query, props.page - 1)}>
          {copy.previous}
        </Link>
      )}
      {pages.map((page, index) =>
        page === null ? (
          <span className={styles.ellipsis} key={`gap-${index}`}>
            …
          </span>
        ) : page === props.page ? (
          <span aria-current="page" className={styles.currentPage} key={page}>
            {page}
          </span>
        ) : (
          <Link className={styles.paginationButton} href={pageHref(props.basePath, props.query, page)} key={page}>
            {page}
          </Link>
        ),
      )}
      {nextDisabled ? (
        <button className={styles.paginationButton} disabled type="button">
          {copy.next}
        </button>
      ) : (
        <Link className={styles.paginationButton} href={pageHref(props.basePath, props.query, props.page + 1)}>
          {copy.next}
        </Link>
      )}
    </nav>
  );
}

function pageHref(basePath: string, query: RepositoryIssueQuery | RepositoryMergeRequestQuery, page: number): string {
  const params = repositoryWorkItemSearchParams(query, { page });
  return params.size > 0 ? `${basePath}?${params}` : basePath;
}

function pageRange(current: number, total: number): Array<number | null> {
  if (total <= 1) return [1];
  const pages = new Set<number>();
  pages.add(1);
  pages.add(total);
  for (let p = current - 1; p <= current + 1; p++) {
    if (p >= 1 && p <= total) pages.add(p);
  }
  const sorted = [...pages].sort((a, b) => a - b);
  const result: Array<number | null> = [];
  let prev = 0;
  for (const page of sorted) {
    if (prev && page - prev > 1) result.push(null);
    result.push(page);
    prev = page;
  }
  return result;
}
