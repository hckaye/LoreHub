import { CircleDot, GitPullRequest, Search } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type {
  GlobalWorkItemKind,
  GlobalWorkItemPage,
  GlobalWorkItemQuery,
  GlobalWorkItemScope,
  GlobalWorkItemState,
} from "@/lib/global-work-items";

import { EmptyState } from "../ui/empty-state";
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
  const description = props.kind === "issue" ? copy.issuesDescription : copy.pullRequestsDescription;
  const createLabel =
    props.kind === "issue" ? props.dictionary.common.newIssue : props.dictionary.common.newPullRequest;
  return (
    <div className={styles.page}>
      <header className={styles.heading}>
        <div>
          <h1>{title}</h1>
          <p>{description}</p>
        </div>
        <Link className={styles.create} href={`/${props.locale}/${route}/new`}>
          {createLabel}
        </Link>
      </header>
      <WorkItemFilters {...props} route={route} />
      {props.unavailable ? (
        <EmptyState
          body={copy.unavailableBody}
          icon={<CircleDot aria-hidden="true" />}
          title={copy.unavailableTitle}
          tone="warning"
        />
      ) : !props.page || props.page.items.length === 0 ? (
        <EmptyState
          body={props.kind === "issue" ? copy.noIssuesBody : copy.noPullRequestsBody}
          icon={props.kind === "issue" ? <CircleDot aria-hidden="true" /> : <GitPullRequest aria-hidden="true" />}
          title={props.kind === "issue" ? copy.noIssuesTitle : copy.noPullRequestsTitle}
        />
      ) : (
        <>
          <div className={styles.list}>
            {props.page.items.map((item) => (
              <GlobalWorkItemRow dictionary={props.dictionary} item={item} key={item.id} locale={props.locale} />
            ))}
          </div>
          {props.page.nextCursor && (
            <div className={styles.pagination}>
              <Link href={workItemHref(props.locale, route, props.query, { cursor: props.page.nextCursor })}>
                {copy.nextPage}
              </Link>
            </div>
          )}
        </>
      )}
    </div>
  );
}

type WorkItemFiltersProps = GlobalWorkItemListProps & { route: string };

function WorkItemFilters(props: WorkItemFiltersProps) {
  const copy = props.dictionary.globalWorkItems;
  const states: Array<[GlobalWorkItemState, string]> = [
    ["open", props.dictionary.common.open],
    ["closed", props.dictionary.common.closed],
  ];
  if (props.kind === "pull_request") states.push(["merged", props.dictionary.common.merged]);
  states.push(["all", props.dictionary.common.all]);
  const scopes: Array<[GlobalWorkItemScope, string]> = [
    ["involved", copy.involved],
    ["created", copy.created],
    ["assigned", copy.assigned],
  ];
  if (props.kind === "pull_request") scopes.push(["review_requested", copy.reviewRequested]);
  scopes.push(["all", copy.allVisible]);
  return (
    <section aria-label={props.dictionary.common.filter} className={styles.filters}>
      <div className={styles.filterRow}>
        <div className={styles.tabGroup} role="tablist" aria-label={copy.scopeLabel}>
          {scopes.map(([value, label]) => (
            <Link
              aria-current={props.query.scope === value ? "page" : undefined}
              className={props.query.scope === value ? styles.tabActive : styles.tab}
              href={workItemHref(props.locale, props.route, props.query, { scope: value, cursor: undefined })}
              key={value}
              role="tab"
            >
              {label}
            </Link>
          ))}
        </div>
        <form action={`/${props.locale}/${props.route}`} className={styles.search} role="search">
          <input name="state" type="hidden" value={props.query.state} />
          <input name="scope" type="hidden" value={props.query.scope} />
          <Search aria-hidden="true" className={styles.searchIcon} size={14} />
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
      <div className={styles.stateRow}>
        {states.map(([value, label]) => (
          <Link
            aria-current={props.query.state === value ? "page" : undefined}
            className={props.query.state === value ? styles.stateActive : styles.stateLink}
            href={workItemHref(props.locale, props.route, props.query, { state: value, cursor: undefined })}
            key={value}
          >
            {label}
          </Link>
        ))}
      </div>
    </section>
  );
}

function workItemHref(
  locale: Locale,
  route: string,
  query: GlobalWorkItemQuery,
  overrides: Partial<Record<"state" | "scope" | "q" | "cursor", string | undefined>>,
): string {
  const params = new URLSearchParams();
  const values = { ...query, ...overrides };
  for (const key of ["state", "scope", "q", "cursor"] as const) {
    const v = values[key];
    if (v && v !== "open" && v !== "involved") params.set(key, v);
  }
  const search = params.toString();
  return `/${locale}/${route}${search ? `?${search}` : ""}`;
}
