import { CircleDot, GitMerge, GitPullRequest, MessageSquare, ThumbsUp } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type {
  GlobalWorkItem,
  GlobalWorkItemKind,
  GlobalWorkItemPage,
  GlobalWorkItemQuery,
  GlobalWorkItemScope,
  GlobalWorkItemState,
} from "@/lib/global-work-items";
import { repositoryPath } from "@/lib/routes";

import { EmptyState } from "../ui/empty-state";
import styles from "./global-work-item-list.module.css";

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
    <main className={styles.page}>
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
              <WorkItemRow dictionary={props.dictionary} item={item} key={item.id} locale={props.locale} />
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
    </main>
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
      <FilterLinks
        active={props.query.state}
        label={copy.stateLabel}
        links={states.map(([value, label]) => ({
          href: workItemHref(props.locale, props.route, props.query, { state: value, cursor: undefined }),
          label,
          value,
        }))}
      />
      <FilterLinks
        active={props.query.scope}
        label={copy.scopeLabel}
        links={scopes.map(([value, label]) => ({
          href: workItemHref(props.locale, props.route, props.query, { scope: value, cursor: undefined }),
          label,
          value,
        }))}
      />
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
        <button type="submit">{copy.searchButton}</button>
      </form>
    </section>
  );
}

type FilterLinksProps = {
  active: string;
  label: string;
  links: Array<{ href: string; label: string; value: string }>;
};

function FilterLinks(props: FilterLinksProps) {
  return (
    <nav aria-label={props.label} className={styles.filterLinks}>
      <span>{props.label}</span>
      {props.links.map((link) => (
        <Link aria-current={props.active === link.value ? "page" : undefined} href={link.href} key={link.value}>
          {link.label}
        </Link>
      ))}
    </nav>
  );
}

function WorkItemRow(props: { dictionary: Dictionary; item: GlobalWorkItem; locale: Locale }) {
  const { dictionary, item, locale } = props;
  const section = item.kind === "issue" ? "issues" : "pulls";
  const itemHref = `${repositoryPath(locale, item.repository.owner, item.repository.slug, section)}/${item.number}`;
  const repoHref = repositoryPath(locale, item.repository.owner, item.repository.slug);
  const repository = `${item.repository.owner}/${item.repository.slug}`;
  const metadata = dictionary.globalWorkItems.itemMetadata
    .replace("{number}", String(item.number))
    .replace("{author}", item.author.username)
    .replace("{date}", formatDate(item.updatedAt, locale));
  return (
    <article className={styles.row} data-state={item.state}>
      {item.kind === "issue" ? (
        <CircleDot aria-hidden="true" className={styles.stateIcon} size={18} />
      ) : (
        <GitPullRequest aria-hidden="true" className={styles.stateIcon} size={18} />
      )}
      <div className={styles.content}>
        <h2>
          <Link href={itemHref}>{item.title}</Link>
          {item.isDraft && <span className={styles.draft}>{dictionary.globalWorkItems.draft}</span>}
        </h2>
        <p>
          <Link href={repoHref}>{repository}</Link> {metadata}
        </p>
        {item.kind === "pull_request" && (
          <div className={styles.branches}>
            <code>{item.sourceBranch}</code>
            <GitMerge aria-hidden="true" size={13} />
            <code>{item.targetBranch}</code>
          </div>
        )}
        {item.labels.length > 0 && (
          <div className={styles.labels}>
            {item.labels.map((label) => (
              <span key={label.id} style={{ borderColor: `#${label.color}` }}>
                {label.name}
              </span>
            ))}
          </div>
        )}
      </div>
      <div className={styles.trailing}>
        {item.assignees.length > 0 && (
          <div className={styles.assignees}>
            {item.assignees.slice(0, 3).map((assignee) => (
              <span key={assignee.id} title={`@${assignee.username}`}>
                {[...(assignee.displayName || assignee.username)][0]?.toUpperCase() ?? "?"}
              </span>
            ))}
          </div>
        )}
        {item.commentCount > 0 && (
          <span title={dictionary.globalWorkItems.comments.replace("{count}", String(item.commentCount))}>
            <MessageSquare aria-hidden="true" size={14} /> {item.commentCount}
          </span>
        )}
        {item.kind === "pull_request" && item.approvalCount > 0 && (
          <span title={dictionary.globalWorkItems.approvals.replace("{count}", String(item.approvalCount))}>
            <ThumbsUp aria-hidden="true" size={14} /> {item.approvalCount}
          </span>
        )}
      </div>
    </article>
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
    if (values[key]) params.set(key, values[key]);
  }
  return `/${locale}/${route}?${params.toString()}`;
}

function formatDate(value: string, locale: Locale): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(new Date(value));
}
