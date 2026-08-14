"use client";

import { Check, ChevronDown, CircleDot, GitMerge, Milestone as MilestoneIcon, Search, Tag } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type FormEvent, type ReactNode } from "react";

import { PopupMenu } from "@/components/ui/popup-menu";
import type { Dictionary } from "@/i18n";
import type { Label, Milestone } from "@/lib/api-types";
import { normalizeLabelColor } from "@/lib/format";
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
type NavigateFn = (changes: QueryChanges) => void;

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
        <TextFilterDropdown
          copy={copy}
          currentValue={props.query.author}
          filterLabel={copy.filterByAuthor}
          key={`author-${props.query.author ?? ""}`}
          label={copy.author}
          navigate={navigate}
          paramName="author"
          placeholder={copy.authorPlaceholder}
        />
        <TextFilterDropdown
          copy={copy}
          currentValue={props.query.assignee}
          filterLabel={copy.filterByAssignee}
          key={`assignee-${props.query.assignee ?? ""}`}
          label={copy.assignee}
          navigate={navigate}
          paramName="assignee"
          placeholder={copy.assigneePlaceholder}
        />
        <LabelFilterDropdown
          copy={copy}
          currentLabels={props.query.labels ?? []}
          labels={props.labels}
          navigate={navigate}
        />
        <MilestoneFilterDropdown
          copy={copy}
          currentMilestone={props.query.milestone}
          milestones={props.milestones}
          navigate={navigate}
        />
        <SortDropdown
          copy={copy}
          currentDirection={props.query.direction ?? "desc"}
          currentSort={props.query.sort ?? "updated"}
          navigate={navigate}
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

function TextFilterDropdown(props: {
  copy: Dictionary["workItemLists"];
  currentValue?: string;
  filterLabel: string;
  label: string;
  navigate: NavigateFn;
  paramName: "author" | "assignee";
  placeholder: string;
}) {
  const [value, setValue] = useState(props.currentValue ?? "");
  const displayValue = props.currentValue ? props.currentValue : props.label;

  return (
    <PopupMenu
      className={styles.dropdown}
      panelClassName={styles.dropdownMenu}
      panelRole="none"
      trigger={<DropdownTriggerContent label={displayValue} />}
      triggerClassName={styles.dropdownTrigger}
    >
      {(close) => (
        <>
          <div className={styles.dropdownHeader}>{props.filterLabel}</div>
          <form
            className={styles.dropdownForm}
            onSubmit={(event) => {
              event.preventDefault();
              const trimmed = value.trim();
              props.navigate({ [props.paramName]: trimmed || undefined } as QueryChanges);
              close();
            }}
          >
            <input
              aria-label={props.filterLabel}
              autoFocus
              className={styles.dropdownInput}
              maxLength={100}
              onChange={(event) => setValue(event.target.value)}
              placeholder={props.placeholder}
              type="text"
              value={value}
            />
            <button className={styles.dropdownSubmit} type="submit">
              {props.copy.apply}
            </button>
          </form>
        </>
      )}
    </PopupMenu>
  );
}

function DropdownTriggerContent({ label }: { label: string }) {
  return (
    <>
      <span className={styles.dropdownTriggerLabel}>{label}</span>
      <ChevronDown aria-hidden="true" className={styles.dropdownCaret} size={14} />
    </>
  );
}

function LabelFilterDropdown(props: {
  copy: Dictionary["workItemLists"];
  currentLabels: string[];
  labels: Label[];
  navigate: NavigateFn;
}) {
  const [filter, setFilter] = useState("");
  const normalizedFilter = filter.trim().toLocaleLowerCase("en");
  const visibleLabels = props.labels.filter((label) => label.name.toLocaleLowerCase("en").includes(normalizedFilter));
  const displayValue = props.currentLabels.length > 0 ? props.currentLabels.join(", ") : props.copy.label;

  function isSelected(name: string): boolean {
    const normalizedName = name.toLocaleLowerCase("en");
    return props.currentLabels.some((selected) => selected.toLocaleLowerCase("en") === normalizedName);
  }

  function clear(close: () => void) {
    props.navigate({ labels: undefined });
    setFilter("");
    close();
  }

  function toggle(name: string, close: () => void) {
    const nextLabels = isSelected(name)
      ? props.currentLabels.filter((selected) => selected.toLocaleLowerCase("en") !== name.toLocaleLowerCase("en"))
      : [...props.currentLabels, name];
    props.navigate({ labels: nextLabels.length > 0 ? nextLabels : undefined });
    setFilter("");
    close();
  }

  return (
    <PopupMenu
      className={styles.dropdown}
      panelClassName={styles.dropdownMenu}
      panelRole="none"
      trigger={<DropdownTriggerContent label={displayValue} />}
      triggerClassName={styles.dropdownTrigger}
    >
      {(close) => (
        <>
          <div className={styles.dropdownHeader}>{props.copy.filterByLabel}</div>
          <input
            aria-label={props.copy.filterByLabel}
            autoFocus
            className={styles.dropdownInput}
            onChange={(event) => setFilter(event.target.value)}
            placeholder={props.copy.labelPlaceholder}
            type="search"
            value={filter}
          />
          <button
            aria-pressed={props.currentLabels.length === 0}
            className={styles.dropdownItem}
            data-active={props.currentLabels.length === 0 ? "true" : undefined}
            onClick={() => clear(close)}
            type="button"
          >
            <span className={styles.dropdownOptionLabel}>{props.copy.anyLabel}</span>
            <span aria-hidden="true" className={styles.dropdownCheck}>
              {props.currentLabels.length === 0 && <Check size={16} />}
            </span>
          </button>
          <div className={styles.dropdownDivider} />
          <div aria-label={props.copy.filterByLabel} className={styles.dropdownOptions} role="listbox">
            {visibleLabels.length > 0 ? (
              visibleLabels.map((label) => {
                const selected = isSelected(label.name);
                return (
                  <button
                    aria-pressed={selected}
                    className={styles.dropdownItem}
                    data-active={selected ? "true" : undefined}
                    key={label.id}
                    onClick={() => toggle(label.name, close)}
                    type="button"
                  >
                    <span
                      aria-hidden="true"
                      className={styles.labelDot}
                      style={{ backgroundColor: normalizeLabelColor(label.color) }}
                    />
                    <span className={styles.dropdownOptionLabel}>{label.name}</span>
                    <span aria-hidden="true" className={styles.dropdownCheck}>
                      {selected && <Check size={16} />}
                    </span>
                  </button>
                );
              })
            ) : (
              <p className={styles.dropdownEmpty}>{props.copy.noLabels}</p>
            )}
          </div>
        </>
      )}
    </PopupMenu>
  );
}

function MilestoneFilterDropdown(props: {
  copy: Dictionary["workItemLists"];
  currentMilestone?: string;
  milestones: Milestone[];
  navigate: NavigateFn;
}) {
  const [filter, setFilter] = useState("");
  const normalizedFilter = filter.trim().toLocaleLowerCase("en");
  const visibleMilestones = props.milestones.filter((milestone) =>
    milestone.title.toLocaleLowerCase("en").includes(normalizedFilter),
  );
  const selectedMilestone = props.milestones.find((milestone) => String(milestone.number) === props.currentMilestone);
  const displayValue =
    props.currentMilestone === "none"
      ? props.copy.noMilestone
      : (selectedMilestone?.title ?? props.currentMilestone ?? props.copy.milestone);

  function select(value: string | undefined, close: () => void) {
    props.navigate({ milestone: value });
    setFilter("");
    close();
  }

  return (
    <PopupMenu
      className={styles.dropdown}
      panelClassName={styles.dropdownMenu}
      panelRole="none"
      trigger={<DropdownTriggerContent label={displayValue} />}
      triggerClassName={styles.dropdownTrigger}
    >
      {(close) => (
        <>
          <div className={styles.dropdownHeader}>{props.copy.filterByMilestone}</div>
          <input
            aria-label={props.copy.filterByMilestone}
            autoFocus
            className={styles.dropdownInput}
            onChange={(event) => setFilter(event.target.value)}
            placeholder={props.copy.milestonePlaceholder}
            type="search"
            value={filter}
          />
          <button
            aria-pressed={props.currentMilestone === undefined}
            className={styles.dropdownItem}
            data-active={props.currentMilestone === undefined ? "true" : undefined}
            onClick={() => select(undefined, close)}
            type="button"
          >
            <span className={styles.dropdownOptionLabel}>{props.copy.anyMilestone}</span>
            <span aria-hidden="true" className={styles.dropdownCheck}>
              {props.currentMilestone === undefined && <Check size={16} />}
            </span>
          </button>
          <div className={styles.dropdownDivider} />
          <div aria-label={props.copy.filterByMilestone} className={styles.dropdownOptions} role="listbox">
            {props.copy.noMilestone.toLocaleLowerCase("en").includes(normalizedFilter) && (
              <button
                aria-pressed={props.currentMilestone === "none"}
                className={styles.dropdownItem}
                data-active={props.currentMilestone === "none" ? "true" : undefined}
                onClick={() => select("none", close)}
                type="button"
              >
                <span className={styles.dropdownOptionLabel}>{props.copy.noMilestone}</span>
                <span aria-hidden="true" className={styles.dropdownCheck}>
                  {props.currentMilestone === "none" && <Check size={16} />}
                </span>
              </button>
            )}
            {visibleMilestones.map((milestone) => {
              const selected = String(milestone.number) === props.currentMilestone;
              return (
                <button
                  aria-pressed={selected}
                  className={styles.dropdownItem}
                  data-active={selected ? "true" : undefined}
                  key={milestone.id}
                  onClick={() => select(String(milestone.number), close)}
                  type="button"
                >
                  <span className={styles.dropdownOptionLabel}>{milestone.title}</span>
                  <span aria-hidden="true" className={styles.dropdownCheck}>
                    {selected && <Check size={16} />}
                  </span>
                </button>
              );
            })}
            {visibleMilestones.length === 0 && <p className={styles.dropdownEmpty}>{props.copy.noOpenMilestones}</p>}
          </div>
        </>
      )}
    </PopupMenu>
  );
}

function SortDropdown(props: {
  copy: Dictionary["workItemLists"];
  currentDirection: RepositoryWorkItemDirection;
  currentSort: RepositoryWorkItemSort;
  navigate: NavigateFn;
}) {
  const sortLabel =
    props.currentSort === "created"
      ? props.copy.sortCreated
      : props.currentSort === "comments"
        ? props.copy.sortComments
        : props.copy.sortUpdated;

  function select(changes: QueryChanges, close: () => void) {
    props.navigate(changes);
    close();
  }

  return (
    <PopupMenu
      className={styles.dropdown}
      panelClassName={styles.dropdownMenu}
      panelRole="none"
      trigger={<DropdownTriggerContent label={sortLabel} />}
      triggerClassName={styles.dropdownTrigger}
    >
      {(close) => (
        <>
          <SortOption
            copy={props.copy.sortUpdated}
            current={props.currentSort}
            onSelect={(changes) => select(changes, close)}
            sort="updated"
          />
          <SortOption
            copy={props.copy.sortCreated}
            current={props.currentSort}
            onSelect={(changes) => select(changes, close)}
            sort="created"
          />
          <SortOption
            copy={props.copy.sortComments}
            current={props.currentSort}
            onSelect={(changes) => select(changes, close)}
            sort="comments"
          />
          <div className={styles.dropdownDivider} />
          <SortOption
            copy={props.copy.descending}
            current={props.currentDirection}
            onSelect={(changes) => select(changes, close)}
            sort="direction-desc"
          />
          <SortOption
            copy={props.copy.ascending}
            current={props.currentDirection}
            onSelect={(changes) => select(changes, close)}
            sort="direction-asc"
          />
        </>
      )}
    </PopupMenu>
  );
}

function SortOption(props: {
  copy: string;
  current: string;
  onSelect: (changes: QueryChanges) => void;
  sort: RepositoryWorkItemSort | `direction-${RepositoryWorkItemDirection}`;
}) {
  const isActive =
    props.sort === props.current ||
    (props.sort === `direction-${props.current}` && props.sort.startsWith("direction-"));
  return (
    <button
      className={styles.dropdownItem}
      data-active={isActive ? "true" : undefined}
      onClick={() => {
        if (props.sort.startsWith("direction-")) {
          const direction = props.sort.replace("direction-", "") as RepositoryWorkItemDirection;
          props.onSelect({ direction });
        } else {
          props.onSelect({ sort: props.sort as RepositoryWorkItemSort });
        }
      }}
      type="button"
    >
      <span className={styles.dropdownOptionLabel}>{props.copy}</span>
      <span aria-hidden="true" className={styles.dropdownCheck}>
        {isActive && <Check size={16} />}
      </span>
    </button>
  );
}

function queryHref(basePath: string, query: Query, changes: QueryChanges): string {
  const params = repositoryWorkItemSearchParams(query, { ...changes, page: 1 });
  return params.size > 0 ? `${basePath}?${params}` : basePath;
}
