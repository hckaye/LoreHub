import { Search } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { RepositoryIssueQuery, RepositoryMergeRequestQuery } from "@/lib/repository-work-item-query";
import { repositoryWorkItemSearchParams } from "@/lib/repository-work-item-query";

import styles from "./work-item-list-toolbar.module.css";

type WorkItemListToolbarProps = {
  basePath: string;
  dictionary: Dictionary;
  kind: "issues" | "pulls";
  query: RepositoryIssueQuery | RepositoryMergeRequestQuery;
  totalCount?: number;
};

export function WorkItemListToolbar(props: WorkItemListToolbarProps) {
  const copy = props.dictionary.workItemLists;
  const clear = repositoryWorkItemSearchParams({ state: props.query.state });
  const clearHref = clear.size > 0 ? `${props.basePath}?${clear}` : props.basePath;
  return (
    <section aria-label={copy.filters} className={styles.toolbar}>
      <div className={styles.summary}>
        {props.totalCount !== undefined && <strong>{copy.results.replace("{count}", String(props.totalCount))}</strong>}
        <Link href={clearHref}>{copy.clear}</Link>
      </div>
      <form action={props.basePath} className={styles.form} method="get">
        {props.query.state && props.query.state !== "open" && (
          <input name="state" type="hidden" value={props.query.state} />
        )}
        <label className={styles.searchField}>
          <span>{copy.search}</span>
          <div>
            <Search aria-hidden="true" size={16} />
            <input
              defaultValue={props.query.q}
              maxLength={256}
              name="q"
              placeholder={copy.searchPlaceholder}
              type="search"
            />
          </div>
        </label>
        <FilterInput
          defaultValue={props.query.author}
          label={copy.author}
          name="author"
          placeholder={copy.authorPlaceholder}
        />
        <FilterInput
          defaultValue={props.query.assignee}
          label={copy.assignee}
          name="assignee"
          placeholder={copy.assigneePlaceholder}
        />
        <FilterInput
          defaultValue={props.query.labels?.join(", ")}
          label={copy.label}
          name="label"
          placeholder={copy.labelPlaceholder}
        />
        <FilterInput
          defaultValue={props.query.milestone}
          label={copy.milestone}
          name="milestone"
          placeholder={copy.milestonePlaceholder}
        />
        {props.kind === "pulls" && isPullRequestQuery(props.query) && (
          <PullRequestFilters dictionary={props.dictionary} query={props.query} />
        )}
        <label>
          <span>{copy.sort}</span>
          <select defaultValue={props.query.sort ?? "updated"} name="sort">
            <option value="updated">{copy.sortUpdated}</option>
            <option value="created">{copy.sortCreated}</option>
            <option value="comments">{copy.sortComments}</option>
          </select>
        </label>
        <label>
          <span>{copy.direction}</span>
          <select defaultValue={props.query.direction ?? "desc"} name="direction">
            <option value="desc">{copy.descending}</option>
            <option value="asc">{copy.ascending}</option>
          </select>
        </label>
        <button type="submit">{copy.apply}</button>
      </form>
    </section>
  );
}

function FilterInput(props: {
  defaultValue?: string;
  label: string;
  maxLength?: number;
  name: string;
  placeholder: string;
}) {
  return (
    <label>
      <span>{props.label}</span>
      <input
        defaultValue={props.defaultValue}
        maxLength={props.maxLength ?? 100}
        name={props.name}
        placeholder={props.placeholder}
      />
    </label>
  );
}

function PullRequestFilters(props: { dictionary: Dictionary; query: RepositoryMergeRequestQuery }) {
  const copy = props.dictionary.workItemLists;
  return (
    <>
      <FilterInput
        defaultValue={props.query.source}
        label={copy.sourceBranch}
        maxLength={255}
        name="source"
        placeholder={copy.branchPlaceholder}
      />
      <FilterInput
        defaultValue={props.query.target}
        label={copy.targetBranch}
        maxLength={255}
        name="target"
        placeholder={copy.branchPlaceholder}
      />
      <label>
        <span>{copy.draft}</span>
        <select defaultValue={props.query.draft === undefined ? "" : String(props.query.draft)} name="draft">
          <option value="">{copy.draftAny}</option>
          <option value="true">{copy.draftOnly}</option>
          <option value="false">{copy.readyOnly}</option>
        </select>
      </label>
    </>
  );
}

function isPullRequestQuery(
  query: RepositoryIssueQuery | RepositoryMergeRequestQuery,
): query is RepositoryMergeRequestQuery {
  return "source" in query || "target" in query || "draft" in query;
}
