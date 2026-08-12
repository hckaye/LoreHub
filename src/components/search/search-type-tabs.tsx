import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import { searchHref, searchTypeCount, searchTypes, type SearchCounts, type SearchQuery } from "@/lib/search";

import styles from "./search-page.module.css";

type SearchTypeTabsProps = {
  counts: SearchCounts;
  dictionary: Dictionary;
  locale: Locale;
  query: SearchQuery;
};

export function SearchTypeTabs({ counts, dictionary, locale, query }: SearchTypeTabsProps) {
  return (
    <nav aria-label={dictionary.searchPage.filtersLabel} className={styles.tabs}>
      {searchTypes.map((type) => (
        <Link
          aria-current={query.type === type ? "page" : undefined}
          href={searchHref(locale, query, { type, page: 1 })}
          key={type}
        >
          <span>{dictionary.searchPage[type]}</span>
          <span className={styles.count}>{searchTypeCount(counts, type)}</span>
        </Link>
      ))}
    </nav>
  );
}
