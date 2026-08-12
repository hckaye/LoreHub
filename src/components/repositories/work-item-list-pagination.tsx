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
  query: RepositoryIssueQuery | RepositoryMergeRequestQuery;
};

export function WorkItemListPagination(props: WorkItemListPaginationProps) {
  const copy = props.dictionary.workItemLists;
  return (
    <nav aria-label={copy.page.replace("{page}", String(props.page))} className={styles.pagination}>
      {props.page > 1 ? (
        <Link href={pageHref(props.basePath, props.query, props.page - 1)}>{copy.previous}</Link>
      ) : (
        <span aria-disabled="true">{copy.previous}</span>
      )}
      <strong>{copy.page.replace("{page}", String(props.page))}</strong>
      {props.hasNext ? (
        <Link href={pageHref(props.basePath, props.query, props.page + 1)}>{copy.next}</Link>
      ) : (
        <span aria-disabled="true">{copy.next}</span>
      )}
    </nav>
  );
}

function pageHref(basePath: string, query: RepositoryIssueQuery | RepositoryMergeRequestQuery, page: number): string {
  const params = repositoryWorkItemSearchParams(query, { page });
  return `${basePath}?${params}`;
}
