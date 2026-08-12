import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import { searchHref, type SearchQuery } from "@/lib/search";

import styles from "./search-page.module.css";

type SearchPaginationProps = {
  dictionary: Dictionary;
  lastPage: number;
  locale: Locale;
  query: SearchQuery;
};

export function SearchPagination({ dictionary, lastPage, locale, query }: SearchPaginationProps) {
  if (lastPage <= 1) return null;
  return (
    <nav aria-label={dictionary.searchPage.paginationLabel} className={styles.pagination}>
      {query.page > 1 ? (
        <Link href={searchHref(locale, query, { page: query.page - 1 })} rel="prev">
          {dictionary.searchPage.previousPage}
        </Link>
      ) : (
        <span aria-disabled="true">{dictionary.searchPage.previousPage}</span>
      )}
      <p>
        {dictionary.searchPage.pageStatus.replace("{page}", String(query.page)).replace("{pages}", String(lastPage))}
      </p>
      {query.page < lastPage ? (
        <Link href={searchHref(locale, query, { page: query.page + 1 })} rel="next">
          {dictionary.searchPage.nextPage}
        </Link>
      ) : (
        <span aria-disabled="true">{dictionary.searchPage.nextPage}</span>
      )}
    </nav>
  );
}
