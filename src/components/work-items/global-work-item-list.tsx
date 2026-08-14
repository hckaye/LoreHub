import { Check, CircleDot } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type {
  GlobalWorkItemKind,
  GlobalWorkItemPage,
  GlobalWorkItemQuery,
  GlobalWorkItemScope,
} from "@/lib/global-work-items";

import styles from "./global-work-item-list.module.css";
import { GlobalWorkItemRow } from "./global-work-item-row";

type GlobalWorkItemListProps = {
  dictionary: Dictionary;
  kind: GlobalWorkItemKind;
  locale: Locale;
  page: GlobalWorkItemPage | null;
  query: GlobalWorkItemQuery;
  unavailable: boolean;
};

export function GlobalWorkItemList(props: GlobalWorkItemListProps) {
  const copy = props.dictionary.globalWorkItems;
  const route = props.kind === "issue" ? "issues" : "pulls";
  const title = props.kind === "issue" ? copy.issuesTitle : copy.pullRequestsTitle;
  return (
    <div className={styles.page}>
      <h1 className="visually-hidden">{title}</h1>
      <WorkItemFilters {...props} route={route} />
      <div className={styles.box}>
        <WorkItemBoxHeader copy={copy} locale={props.locale} page={props.page} query={props.query} route={route} />
        <WorkItemBoxBody {...props} />
      </div>
      {props.page?.nextCursor || props.query.page > 1 ? (
        <WorkItemPagination
          copy={copy}
          locale={props.locale}
          nextCursor={props.page?.nextCursor ?? null}
          query={props.query}
          route={route}
        />
      ) : null}
    </div>
  );
}

function WorkItemBoxHeader(props: {
  copy: Dictionary["globalWorkItems"];
  locale: Locale;
  page: GlobalWorkItemPage | null;
  query: GlobalWorkItemQuery;
  route: string;
}) {
  const counts = headerCounts(props.page, props.query);
  return (
    <div className={styles.boxHeader}>
      <Link
        aria-current={props.query.state === "open" ? "page" : undefined}
        className={props.query.state === "open" ? styles.stateActive : styles.stateLink}
        href={workItemHref(props.locale, props.route, props.query, { state: "open", cursor: undefined, page: 1 })}
      >
        <CircleDot aria-hidden="true" className={styles.openIcon} size={16} />
        {props.copy.openWithCount.replace("{count}", String(counts.open))}
      </Link>
      <Link
        aria-current={props.query.state === "closed" ? "page" : undefined}
        className={props.query.state === "closed" ? styles.stateActive : styles.stateLink}
        href={workItemHref(props.locale, props.route, props.query, { state: "closed", cursor: undefined, page: 1 })}
      >
        <Check aria-hidden="true" className={styles.closedIcon} size={16} />
        {props.copy.closedWithCount.replace("{count}", String(counts.closed))}
      </Link>
    </div>
  );
}

function WorkItemBoxBody(props: GlobalWorkItemListProps) {
  const copy = props.dictionary.globalWorkItems;
  const items = props.page?.items ?? [];
  if (props.unavailable) {
    return (
      <div className={styles.blank} data-tone="warning">
        <h2>{copy.unavailableTitle}</h2>
        <p>{copy.unavailableBody}</p>
      </div>
    );
  }
  if (items.length === 0) {
    return (
      <div className={styles.blank}>
        <h2>{props.kind === "issue" ? copy.noIssuesTitle : copy.noPullRequestsTitle}</h2>
        <p>{props.kind === "issue" ? copy.noIssuesBody : copy.noPullRequestsBody}</p>
      </div>
    );
  }
  return (
    <div className={styles.list}>
      {items.map((item) => (
        <GlobalWorkItemRow dictionary={props.dictionary} item={item} key={item.id} locale={props.locale} />
      ))}
    </div>
  );
}

type WorkItemFiltersProps = GlobalWorkItemListProps & { route: string };

function WorkItemFilters(props: WorkItemFiltersProps) {
  const copy = props.dictionary.globalWorkItems;
  const scopes: Array<[GlobalWorkItemScope, string]> = [
    ["involved", copy.involved],
    ["created", copy.created],
    ["assigned", copy.assigned],
  ];
  if (props.kind === "pull_request") scopes.push(["review_requested", copy.reviewRequested]);
  scopes.push(["all", copy.allVisible]);
  return (
    <div className={styles.toolbar}>
      <nav aria-label={copy.scopeLabel} className={styles.scopes}>
        {scopes.map(([value, label]) => (
          <Link
            aria-current={props.query.scope === value ? "page" : undefined}
            className={props.query.scope === value ? styles.scopeActive : styles.scope}
            href={workItemHref(props.locale, props.route, props.query, { scope: value, cursor: undefined, page: 1 })}
            key={value}
          >
            {label}
          </Link>
        ))}
      </nav>
      <form action={`/${props.locale}/${props.route}`} className={styles.search} role="search">
        <input name="state" type="hidden" value={props.query.state} />
        <input name="scope" type="hidden" value={props.query.scope} />
        <label className="visually-hidden" htmlFor={`${props.route}-query`}>
          {copy.searchLabel}
        </label>
        <input
          defaultValue={props.query.q ?? ""}
          id={`${props.route}-query`}
          maxLength={160}
          name="q"
          placeholder={copy.searchPlaceholder}
          type="search"
        />
      </form>
    </div>
  );
}

function WorkItemPagination(props: {
  copy: Dictionary["globalWorkItems"];
  locale: Locale;
  nextCursor: string | null;
  query: GlobalWorkItemQuery;
  route: string;
}) {
  const current = props.query.page;
  const pages: Array<number | "gap"> = [1];
  if (current > 2) pages.push("gap");
  if (current !== 1) pages.push(current);
  if (props.nextCursor) pages.push(current + 1);
  const canPrevious = current === 2;
  return (
    <nav aria-label={props.copy.paginationLabel} className={styles.pagination}>
      {canPrevious ? (
        <Link
          className={styles.paginationLink}
          href={workItemHref(props.locale, props.route, props.query, { cursor: undefined, page: 1 })}
          rel="prev"
        >
          {props.copy.previousPage}
        </Link>
      ) : (
        <span className={styles.paginationDisabled}>{props.copy.previousPage}</span>
      )}
      {pages.map((page, index) =>
        page === "gap" ? (
          <span className={styles.paginationGap} key={`gap-${index}`}>
            …
          </span>
        ) : page === current ? (
          <span aria-current="page" className={styles.paginationCurrent} key={page}>
            {page}
          </span>
        ) : (
          <Link
            className={styles.paginationLink}
            href={workItemHref(props.locale, props.route, props.query, pageHref(props.query, page, props.nextCursor))}
            key={page}
          >
            {page}
          </Link>
        ),
      )}
      {props.nextCursor ? (
        <Link
          className={styles.paginationLink}
          href={workItemHref(props.locale, props.route, props.query, {
            cursor: props.nextCursor,
            page: current + 1,
          })}
          rel="next"
        >
          {props.copy.nextPage}
        </Link>
      ) : (
        <span className={styles.paginationDisabled}>{props.copy.nextPage}</span>
      )}
    </nav>
  );
}

function pageHref(
  query: GlobalWorkItemQuery,
  page: number,
  nextCursor: string | null,
): Pick<GlobalWorkItemQuery, "cursor" | "page"> {
  if (page <= 1) return { cursor: undefined, page: 1 };
  return { cursor: nextCursor ?? query.cursor, page };
}

function headerCounts(page: GlobalWorkItemPage | null, query: GlobalWorkItemQuery): { open: number; closed: number } {
  if (!page) return { open: 0, closed: 0 };
  if (typeof page.openCount === "number" && typeof page.closedCount === "number") {
    return { open: page.openCount, closed: page.closedCount };
  }
  if (query.state === "open") return { open: page.items.length, closed: 0 };
  if (query.state === "closed" || query.state === "merged") return { open: 0, closed: page.items.length };
  const open = page.items.filter((item) => item.state === "open").length;
  return { open, closed: page.items.length - open };
}

function workItemHref(
  locale: Locale,
  route: string,
  query: GlobalWorkItemQuery,
  overrides: Partial<Pick<GlobalWorkItemQuery, "state" | "scope" | "q" | "cursor" | "page">>,
): string {
  const values = { ...query, ...overrides };
  const params = new URLSearchParams();
  if (values.state && values.state !== "open") params.set("state", values.state);
  if (values.scope && values.scope !== "involved") params.set("scope", values.scope);
  if (values.q) params.set("q", values.q);
  if (values.cursor) params.set("cursor", values.cursor);
  if (values.page > 1) params.set("page", String(values.page));
  const search = params.toString();
  return `/${locale}/${route}${search ? `?${search}` : ""}`;
}
