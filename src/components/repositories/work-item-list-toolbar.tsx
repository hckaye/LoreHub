"use client";

import { Check, ChevronDown, CircleDot, GitMerge, Milestone as MilestoneIcon, Search, Tag } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type FormEvent, type ReactNode } from "react";

import type { Dictionary } from "@/i18n";
import type { Assignee, Label, Milestone } from "@/lib/api-types";
import type {
  RepositoryIssueQuery,
  RepositoryMergeRequestQuery,
  RepositoryWorkItemDirection,
  RepositoryWorkItemSort,
} from "@/lib/repository-work-item-query";
import { repositoryWorkItemSearchParams } from "@/lib/repository-work-item-query";

import styles from "./work-item-list-toolbar.module.css";

type Query = RepositoryIssueQuery | RepositoryMergeRequestQuery;
type QueryChanges = Partial<RepositoryMergeRequestQuery>;

type WorkItemListToolbarProps = {
  basePath: string;
  createHref?: string;
  createLabel?: string;
  dictionary: Dictionary;
  kind: "issues" | "pulls";
  labels: Label[];
  labelsCount?: number;
  labelsHref: string;
  milestones: Milestone[];
  milestonesCount?: number;
  milestonesHref: string;
  query: Query;
  openCount?: number;
  closedCount?: number;
  mergedCount?: number;
};

export function WorkItemListToolbar(props: WorkItemListToolbarProps) {
  const copy = props.dictionary.workItemLists;
  const [value, setValue] = useState(props.query.q ?? "");
  const router = useRouter();
  const labelsLabel =
    props.labelsCount === undefined
      ? copy.labelsButton
      : copy.labelsButtonWithCount.replace("{count}", String(props.labelsCount));
  const milestonesLabel =
    props.milestonesCount === undefined
      ? copy.milestonesButton
      : copy.milestonesButtonWithCount.replace("{count}", String(props.milestonesCount));

  function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmed = value.trim();
    if (trimmed === (props.query.q ?? "").trim()) return;
    const params = new URLSearchParams(window.location.search);
    params.delete("q");
    params.delete("page");
    if (trimmed) params.set("q", trimmed);
    router.push(params.size > 0 ? `${props.basePath}?${params}` : props.basePath);
  }

  return (
    <section aria-label={copy.filters} className={styles.toolbar}>
      <form className={styles.searchForm} onSubmit={submitSearch} role="search">
        <Search aria-hidden="true" className={styles.searchIcon} size={16} />
        <input
          aria-label={copy.search}
          className={styles.searchInput}
          maxLength={256}
          onChange={(event) => setValue(event.target.value)}
          placeholder={copy.searchPlaceholder}
          type="search"
          value={value}
        />
      </form>

      <div className={styles.actionGroup}>
        <Link className={styles.actionButton} href={props.labelsHref}>
          <Tag aria-hidden="true" size={16} />
          {labelsLabel}
        </Link>
        <Link className={styles.actionButton} href={props.milestonesHref}>
          <MilestoneIcon aria-hidden="true" size={16} />
          {milestonesLabel}
        </Link>
      </div>

      {props.createHref && props.createLabel && (
        <Link className={styles.primaryButton} href={props.createHref}>
          {props.createLabel}
        </Link>
      )}
    </section>
  );
}

type WorkItemListFilterHeaderProps = {
  basePath: string;
  dictionary: Dictionary;
  kind: "issues" | "pulls";
  labels: Label[];
  milestones: Milestone[];
  query: Query;
  authors?: string[];
  assignees?: Assignee[];
  openCount?: number;
  closedCount?: number;
  mergedCount?: number;
};

export function WorkItemListFilterHeader(props: WorkItemListFilterHeaderProps) {
  const copy = props.dictionary.workItemLists;
  const router = useRouter();
  const state = props.query.state ?? "open";
  const openCount = props.openCount ?? 0;
  const closedCount = props.closedCount ?? 0;
  const mergedCount = props.mergedCount ?? 0;

  function navigate(changes: QueryChanges) {
    const params = repositoryWorkItemSearchParams(props.query, { ...changes, page: 1 });
    router.push(params.size > 0 ? `${props.basePath}?${params}` : props.basePath);
  }

  const currentLabel = props.query.labels?.[0] ?? "";
  const authorOptions = uniqueValues([props.query.author, ...(props.authors ?? [])]);
  const assigneeOptions = uniqueAssignees(props.assignees ?? [], props.query.assignee);
  const labelOptions = uniqueValues([currentLabel, ...props.labels.map((label) => label.name)]);
  const milestoneOptions = getMilestoneOptions(props.milestones, props.query.milestone);

  return (
    <div className={styles.listHeader}>
      <StateToggle
        basePath={props.basePath}
        closedCount={closedCount}
        copy={copy}
        isPulls={props.kind === "pulls"}
        mergedCount={mergedCount}
        openCount={openCount}
        query={props.query}
        state={state}
      />

      <div className={styles.filters}>
        <FilterSelect
          ariaLabel={copy.filterByAuthor}
          onChange={(value) => navigate({ author: value || undefined })}
          options={[
            { label: copy.author, value: "" },
            ...authorOptions.map((author) => ({ label: author, value: author })),
          ]}
          value={props.query.author ?? ""}
        />
        <FilterSelect
          ariaLabel={copy.filterByAssignee}
          onChange={(value) => navigate({ assignee: value || undefined })}
          options={[
            { label: copy.assignee, value: "" },
            { label: copy.unassigned, value: "none" },
            ...assigneeOptions.map((assignee) => ({ label: assignee, value: assignee })),
          ]}
          value={props.query.assignee ?? ""}
        />
        <FilterSelect
          ariaLabel={copy.filterByLabel}
          onChange={(value) => navigate({ labels: value ? [value] : undefined })}
          options={[{ label: copy.label, value: "" }, ...labelOptions.map((label) => ({ label, value: label }))]}
          value={currentLabel}
        />
        <FilterSelect
          ariaLabel={copy.filterByMilestone}
          onChange={(value) => navigate({ milestone: value || undefined })}
          options={[
            { label: copy.milestone, value: "" },
            { label: copy.noMilestone, value: "none" },
            ...milestoneOptions.map((milestone) => ({ label: milestone.title, value: String(milestone.number) })),
          ]}
          value={props.query.milestone ?? ""}
        />
        <FilterSelect
          ariaLabel={copy.sort}
          onChange={(value) => {
            const [sort, direction] = value.split(":") as [RepositoryWorkItemSort, RepositoryWorkItemDirection];
            navigate({ direction, sort });
          }}
          options={sortOptions(copy)}
          value={`${props.query.sort ?? "updated"}:${props.query.direction ?? "desc"}`}
        />
      </div>
    </div>
  );
}

function StateToggle(props: {
  basePath: string;
  closedCount: number;
  copy: Dictionary["workItemLists"];
  isPulls: boolean;
  mergedCount: number;
  openCount: number;
  query: Query;
  state: string;
}) {
  return (
    <div className={styles.stateToggle}>
      <StateLink
        active={props.state === "open"}
        basePath={props.basePath}
        icon={<CircleDot aria-hidden="true" size={16} />}
        label={props.copy.openWithCount.replace("{count}", String(props.openCount))}
        query={props.query}
        state="open"
      />
      <StateLink
        active={props.state === "closed"}
        basePath={props.basePath}
        icon={<Check aria-hidden="true" size={16} />}
        label={props.copy.closedWithCount.replace("{count}", String(props.closedCount))}
        query={props.query}
        state="closed"
      />
      {props.isPulls && (
        <StateLink
          active={props.state === "merged"}
          basePath={props.basePath}
          icon={<GitMerge aria-hidden="true" size={16} />}
          label={props.copy.mergedWithCount.replace("{count}", String(props.mergedCount))}
          query={props.query}
          state="merged"
        />
      )}
    </div>
  );
}

function StateLink(props: {
  active: boolean;
  basePath: string;
  icon: ReactNode;
  label: string;
  query: Query;
  state: "open" | "closed" | "merged";
}) {
  return (
    <Link
      aria-current={props.active ? "page" : undefined}
      className={props.active ? styles.stateActive : styles.stateLink}
      href={queryHref(props.basePath, props.query, { state: props.state })}
    >
      {props.icon}
      {props.label}
    </Link>
  );
}

function FilterSelect(props: {
  ariaLabel: string;
  onChange: (value: string) => void;
  options: Array<{ label: string; value: string }>;
  value: string;
}) {
  return (
    <label className={styles.filterSelect}>
      <span className="visually-hidden">{props.ariaLabel}</span>
      <select aria-label={props.ariaLabel} onChange={(event) => props.onChange(event.target.value)} value={props.value}>
        {props.options.map((option) => (
          <option key={`${option.value}-${option.label}`} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
      <ChevronDown aria-hidden="true" className={styles.filterCaret} size={14} />
    </label>
  );
}

function queryHref(basePath: string, query: Query, changes: QueryChanges): string {
  const params = repositoryWorkItemSearchParams(query, { ...changes, page: 1 });
  return params.size > 0 ? `${basePath}?${params}` : basePath;
}

function uniqueValues(values: Array<string | undefined>): string[] {
  return [...new Set(values.filter((value): value is string => Boolean(value)))].sort((a, b) => a.localeCompare(b));
}

function uniqueAssignees(assignees: Assignee[], current?: string): string[] {
  return uniqueValues([current, ...assignees.map((assignee) => assignee.username)]).filter(
    (assignee) => assignee !== "none",
  );
}

function getMilestoneOptions(
  milestones: Milestone[],
  current: string | undefined,
): Array<{ number: number; title: string }> {
  if (!current || current === "none" || milestones.some((milestone) => String(milestone.number) === current)) {
    return milestones;
  }
  return [{ number: Number(current), title: current }, ...milestones];
}

function sortOptions(copy: Dictionary["workItemLists"]): Array<{ label: string; value: string }> {
  return [
    { label: copy.sort, value: "updated:desc" },
    { label: `${copy.sortUpdated} · ${copy.ascending}`, value: "updated:asc" },
    { label: copy.sortCreated, value: "created:desc" },
    { label: `${copy.sortCreated} · ${copy.ascending}`, value: "created:asc" },
    { label: copy.sortComments, value: "comments:desc" },
    { label: `${copy.sortComments} · ${copy.ascending}`, value: "comments:asc" },
  ];
}
