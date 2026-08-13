import { Search as SearchIcon, ShieldAlert, TriangleAlert } from "lucide-react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import {
  lastSearchPage,
  parseCodeSearchQualifier,
  searchTypeCount,
  type SearchQuery,
  type SearchResults,
  type SearchType,
} from "@/lib/search";

import { EmptyState } from "../ui/empty-state";
import { SearchCodeResults } from "./search-code-results";
import { SearchOrganizationResults } from "./search-organization-results";
import styles from "./search-page.module.css";
import { SearchPagination } from "./search-pagination";
import { SearchRepositoryResults } from "./search-repository-results";
import { SearchTypeTabs } from "./search-type-tabs";
import { SearchUserResults } from "./search-user-results";
import { SearchWorkItemResults } from "./search-work-item-results";

export type SearchFailure = "forbidden" | "invalid" | "unavailable" | null;

type SearchPageProps = {
  dictionary: Dictionary;
  failure: SearchFailure;
  locale: Locale;
  query: SearchQuery;
  results: SearchResults | null;
};

export function SearchPage({ dictionary, failure, locale, query, results }: SearchPageProps) {
  return (
    <div className={styles.page}>
      <header className={styles.heading}>
        <h1>{dictionary.searchPage.title}</h1>
        <p>{dictionary.searchPage.description}</p>
        <form action={`/${locale}/search`} className={styles.searchForm} role="search">
          <input name="type" type="hidden" value={query.type} />
          <label className="visually-hidden" htmlFor="cross-resource-search">
            {dictionary.searchPage.inputLabel}
          </label>
          <input
            defaultValue={query.q}
            id="cross-resource-search"
            maxLength={160}
            name="q"
            placeholder={dictionary.searchPage.inputPlaceholder}
            type="search"
          />
          <button type="submit">{dictionary.searchPage.submit}</button>
        </form>
      </header>
      <SearchContent dictionary={dictionary} failure={failure} locale={locale} query={query} results={results} />
    </div>
  );
}

function SearchContent(props: SearchPageProps) {
  return props.query.type === "code" ? <CodeSearchContent {...props} /> : <ResourceSearchContent {...props} />;
}

function CodeSearchContent({ dictionary, failure, locale, query, results }: SearchPageProps) {
  if (!query.q || !parseCodeSearchQualifier(query.q) || failure === "invalid") {
    return (
      <EmptyState
        body={dictionary.searchPage.codeQualifierBody}
        icon={<SearchIcon aria-hidden="true" />}
        title={dictionary.searchPage.codeQualifierTitle}
      />
    );
  }
  if (failure === "forbidden") {
    return (
      <EmptyState
        body={dictionary.searchPage.forbiddenBody}
        icon={<ShieldAlert aria-hidden="true" />}
        title={dictionary.searchPage.forbiddenTitle}
        tone="warning"
      />
    );
  }
  if (failure === "unavailable" || !results?.code) {
    return (
      <EmptyState
        body={dictionary.searchPage.unavailableBody}
        icon={<TriangleAlert aria-hidden="true" />}
        title={dictionary.searchPage.unavailableTitle}
        tone="warning"
      />
    );
  }
  return (
    <>
      <SearchTypeTabs counts={results.counts} dictionary={dictionary} locale={locale} query={query} />
      <div className={styles.results}>
        <SearchCodeResults code={results.code} dictionary={dictionary} locale={locale} query={query} />
      </div>
    </>
  );
}

function ResourceSearchContent({ dictionary, failure, locale, query, results }: SearchPageProps) {
  const selectedCount = results ? searchTypeCount(results.counts, query.type) : 0;
  const lastPage = results ? lastSearchPage(results, query.type) : 1;
  if (!query.q) {
    return (
      <EmptyState
        body={dictionary.searchPage.initialBody}
        icon={<SearchIcon aria-hidden="true" />}
        title={dictionary.searchPage.initialTitle}
      />
    );
  }
  if (failure === "forbidden") {
    return (
      <EmptyState
        body={dictionary.searchPage.forbiddenBody}
        icon={<ShieldAlert aria-hidden="true" />}
        title={dictionary.searchPage.forbiddenTitle}
        tone="warning"
      />
    );
  }
  if (failure !== null || !results) {
    return (
      <EmptyState
        body={dictionary.searchPage.unavailableBody}
        icon={<TriangleAlert aria-hidden="true" />}
        title={dictionary.searchPage.unavailableTitle}
        tone="warning"
      />
    );
  }
  if (selectedCount === 0) {
    return (
      <>
        <SearchTypeTabs counts={results.counts} dictionary={dictionary} locale={locale} query={query} />
        <EmptyState
          body={dictionary.searchPage.emptyBody}
          icon={<SearchIcon aria-hidden="true" />}
          title={dictionary.searchPage.emptyTitle}
        />
      </>
    );
  }
  return (
    <>
      <SearchTypeTabs counts={results.counts} dictionary={dictionary} locale={locale} query={query} />
      <ResourceSearchResults dictionary={dictionary} locale={locale} query={query} results={results} />
      <SearchPagination dictionary={dictionary} lastPage={lastPage} locale={locale} query={query} />
    </>
  );
}

function ResourceSearchResults({ dictionary, locale, query, results }: ResourceSearchResultsProps) {
  return (
    <div className={styles.results}>
      {shows(query.type, "repositories") && (
        <SearchRepositoryResults
          count={results.counts.repositories}
          dictionary={dictionary}
          locale={locale}
          repositories={results.repositories}
        />
      )}
      {shows(query.type, "organizations") && (
        <SearchOrganizationResults
          count={results.counts.organizations}
          dictionary={dictionary}
          locale={locale}
          organizations={results.organizations}
        />
      )}
      {shows(query.type, "users") && (
        <SearchUserResults count={results.counts.users} dictionary={dictionary} locale={locale} users={results.users} />
      )}
      {shows(query.type, "issues") && (
        <SearchWorkItemResults
          count={results.counts.issues}
          dictionary={dictionary}
          items={results.issues}
          kind="issues"
          locale={locale}
        />
      )}
      {shows(query.type, "pulls") && (
        <SearchWorkItemResults
          count={results.counts.pullRequests}
          dictionary={dictionary}
          items={results.pullRequests}
          kind="pulls"
          locale={locale}
        />
      )}
    </div>
  );
}

type ResourceSearchResultsProps = Pick<SearchPageProps, "dictionary" | "locale" | "query"> & {
  results: SearchResults;
};

function shows(active: SearchType, section: Exclude<SearchType, "all">): boolean {
  return active === "all" || active === section;
}
