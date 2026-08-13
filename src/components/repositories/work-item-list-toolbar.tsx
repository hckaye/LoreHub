"use client";

import { Check, ChevronDown, CircleDot, Search } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState, type RefObject } from "react";

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

type WorkItemListToolbarProps = {
  basePath: string;
  dictionary: Dictionary;
  kind: "issues" | "pulls";
  labels: Label[];
  milestones: Milestone[];
  query: RepositoryIssueQuery | RepositoryMergeRequestQuery;
  openCount?: number;
  closedCount?: number;
  mergedCount?: number;
};

export function WorkItemListToolbar(props: WorkItemListToolbarProps) {
  const copy = props.dictionary.workItemLists;
  const router = useRouter();
  const state = props.query.state ?? "open";
  const isPulls = props.kind === "pulls";
  const openCount = props.openCount ?? 0;
  const closedCount = props.closedCount ?? 0;
  const mergedCount = props.mergedCount ?? 0;

  function navigate(changes: Partial<RepositoryMergeRequestQuery>) {
    const params = repositoryWorkItemSearchParams(props.query, { ...changes, page: 1 });
    router.push(params.size > 0 ? `${props.basePath}?${params}` : props.basePath);
  }

  return (
    <section aria-label={copy.filters} className={styles.bar}>
      <StateToggle
        closedCount={closedCount}
        copy={copy}
        isPulls={isPulls}
        mergedCount={mergedCount}
        navigate={navigate}
        openCount={openCount}
        state={state}
      />

      <SearchForm basePath={props.basePath} copy={copy} defaultValue={props.query.q} navigate={navigate} />

      <div className={styles.dropdowns}>
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
    </section>
  );
}

type NavigateFn = (changes: Partial<RepositoryMergeRequestQuery>) => void;

function StateToggle(props: {
  closedCount: number;
  copy: Dictionary["workItemLists"];
  isPulls: boolean;
  mergedCount: number;
  navigate: NavigateFn;
  openCount: number;
  state: string;
}) {
  return (
    <div className={styles.stateToggle}>
      <button
        aria-current={props.state === "open" ? "true" : undefined}
        className={props.state === "open" ? styles.stateActive : styles.stateLink}
        onClick={() => props.navigate({ state: "open" })}
        type="button"
      >
        <CircleDot aria-hidden="true" size={16} />
        {props.copy.openWithCount.replace("{count}", String(props.openCount))}
      </button>
      <button
        aria-current={props.state === "closed" ? "true" : undefined}
        className={props.state === "closed" ? styles.stateActive : styles.stateLink}
        onClick={() => props.navigate({ state: "closed" })}
        type="button"
      >
        <Check aria-hidden="true" size={16} />
        {props.copy.closedWithCount.replace("{count}", String(props.closedCount))}
      </button>
      {props.isPulls && (
        <button
          aria-current={props.state === "merged" ? "true" : undefined}
          className={props.state === "merged" ? styles.stateActive : styles.stateLink}
          onClick={() => props.navigate({ state: "merged" })}
          type="button"
        >
          <Check aria-hidden="true" size={16} />
          {props.copy.mergedWithCount.replace("{count}", String(props.mergedCount))}
        </button>
      )}
    </div>
  );
}

function SearchForm(props: {
  basePath: string;
  copy: Dictionary["workItemLists"];
  defaultValue?: string;
  navigate: NavigateFn;
}) {
  const router = useRouter();
  const [value, setValue] = useState(props.defaultValue ?? "");
  return (
    <form
      className={styles.searchForm}
      onSubmit={(event) => {
        event.preventDefault();
        const trimmed = value.trim();
        if (trimmed === (props.defaultValue ?? "").trim()) return;
        const params = new URLSearchParams();
        // Preserve existing params except q and page
        const current = new URLSearchParams(window.location.search);
        for (const [key, val] of current.entries()) {
          if (key !== "q" && key !== "page") params.set(key, val);
        }
        if (trimmed) params.set("q", trimmed);
        router.push(params.size > 0 ? `${props.basePath}?${params}` : props.basePath);
      }}
      role="search"
    >
      <Search aria-hidden="true" className={styles.searchIcon} size={14} />
      <input
        className={styles.searchInput}
        maxLength={256}
        onChange={(event) => setValue(event.target.value)}
        placeholder={props.copy.searchPlaceholder}
        type="search"
        value={value}
      />
    </form>
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
  const ref = useRef<HTMLDetailsElement>(null);
  const [value, setValue] = useState(props.currentValue ?? "");
  useCloseOnOutsideClickAndEscape(ref);
  const displayValue = props.currentValue ? props.currentValue : props.label;

  return (
    <details className={styles.dropdown} ref={ref}>
      <summary className={styles.dropdownTrigger}>
        <span className={styles.dropdownTriggerLabel}>{displayValue}</span>
        <ChevronDown aria-hidden="true" size={14} />
      </summary>
      <div className={styles.dropdownMenu}>
        <div className={styles.dropdownHeader}>{props.filterLabel}</div>
        <form
          className={styles.dropdownForm}
          onSubmit={(event) => {
            event.preventDefault();
            const trimmed = value.trim();
            props.navigate({
              [props.paramName]: trimmed || undefined,
            } as Partial<RepositoryMergeRequestQuery>);
            closeDetails(ref);
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
      </div>
    </details>
  );
}

function LabelFilterDropdown(props: {
  copy: Dictionary["workItemLists"];
  currentLabels: string[];
  labels: Label[];
  navigate: NavigateFn;
}) {
  const ref = useRef<HTMLDetailsElement>(null);
  const [filter, setFilter] = useState("");
  useCloseOnOutsideClickAndEscape(ref);
  const normalizedFilter = filter.trim().toLocaleLowerCase("en");
  const visibleLabels = props.labels.filter((label) => label.name.toLocaleLowerCase("en").includes(normalizedFilter));
  const displayValue = props.currentLabels.length > 0 ? props.currentLabels.join(", ") : props.copy.label;

  function isSelected(name: string): boolean {
    const normalizedName = name.toLocaleLowerCase("en");
    return props.currentLabels.some((selected) => selected.toLocaleLowerCase("en") === normalizedName);
  }

  function clear() {
    props.navigate({ labels: undefined });
    setFilter("");
    closeDetails(ref);
  }

  function toggle(name: string) {
    const nextLabels = isSelected(name)
      ? props.currentLabels.filter((selected) => selected.toLocaleLowerCase("en") !== name.toLocaleLowerCase("en"))
      : [...props.currentLabels, name];
    props.navigate({ labels: nextLabels.length > 0 ? nextLabels : undefined });
    setFilter("");
    closeDetails(ref);
  }

  return (
    <details className={styles.dropdown} ref={ref}>
      <summary className={styles.dropdownTrigger}>
        <span className={styles.dropdownTriggerLabel}>{displayValue}</span>
        <ChevronDown aria-hidden="true" size={14} />
      </summary>
      <div className={styles.dropdownMenu}>
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
          onClick={clear}
          type="button"
        >
          <span className={styles.dropdownOptionLabel}>{props.copy.anyLabel}</span>
          <span aria-hidden="true" className={styles.dropdownCheck}>
            {props.currentLabels.length === 0 && <Check size={14} />}
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
                  onClick={() => toggle(label.name)}
                  type="button"
                >
                  <span
                    aria-hidden="true"
                    className={styles.labelDot}
                    style={{ backgroundColor: normalizeLabelColor(label.color) }}
                  />
                  <span className={styles.dropdownOptionLabel}>{label.name}</span>
                  <span aria-hidden="true" className={styles.dropdownCheck}>
                    {selected && <Check size={14} />}
                  </span>
                </button>
              );
            })
          ) : (
            <p className={styles.dropdownEmpty}>{props.copy.noLabels}</p>
          )}
        </div>
      </div>
    </details>
  );
}

function MilestoneFilterDropdown(props: {
  copy: Dictionary["workItemLists"];
  currentMilestone?: string;
  milestones: Milestone[];
  navigate: NavigateFn;
}) {
  const ref = useRef<HTMLDetailsElement>(null);
  const [filter, setFilter] = useState("");
  useCloseOnOutsideClickAndEscape(ref);
  const normalizedFilter = filter.trim().toLocaleLowerCase("en");
  const visibleMilestones = props.milestones.filter((milestone) =>
    milestone.title.toLocaleLowerCase("en").includes(normalizedFilter),
  );
  const selectedMilestone = props.milestones.find((milestone) => String(milestone.number) === props.currentMilestone);
  const displayValue =
    props.currentMilestone === "none"
      ? props.copy.noMilestone
      : (selectedMilestone?.title ?? props.currentMilestone ?? props.copy.milestone);

  function select(value: string | undefined) {
    props.navigate({ milestone: value });
    setFilter("");
    closeDetails(ref);
  }

  return (
    <details className={styles.dropdown} ref={ref}>
      <summary className={styles.dropdownTrigger}>
        <span className={styles.dropdownTriggerLabel}>{displayValue}</span>
        <ChevronDown aria-hidden="true" size={14} />
      </summary>
      <div className={styles.dropdownMenu}>
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
          onClick={() => select(undefined)}
          type="button"
        >
          <span className={styles.dropdownOptionLabel}>{props.copy.anyMilestone}</span>
          <span aria-hidden="true" className={styles.dropdownCheck}>
            {props.currentMilestone === undefined && <Check size={14} />}
          </span>
        </button>
        <div className={styles.dropdownDivider} />
        <div aria-label={props.copy.filterByMilestone} className={styles.dropdownOptions} role="listbox">
          {props.copy.noMilestone.toLocaleLowerCase("en").includes(normalizedFilter) && (
            <button
              aria-pressed={props.currentMilestone === "none"}
              className={styles.dropdownItem}
              data-active={props.currentMilestone === "none" ? "true" : undefined}
              onClick={() => select("none")}
              type="button"
            >
              <span className={styles.dropdownOptionLabel}>{props.copy.noMilestone}</span>
              <span aria-hidden="true" className={styles.dropdownCheck}>
                {props.currentMilestone === "none" && <Check size={14} />}
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
                onClick={() => select(String(milestone.number))}
                type="button"
              >
                <span className={styles.dropdownOptionLabel}>{milestone.title}</span>
                <span aria-hidden="true" className={styles.dropdownCheck}>
                  {selected && <Check size={14} />}
                </span>
              </button>
            );
          })}
          {visibleMilestones.length === 0 && <p className={styles.dropdownEmpty}>{props.copy.noOpenMilestones}</p>}
        </div>
      </div>
    </details>
  );
}

function SortDropdown(props: {
  copy: Dictionary["workItemLists"];
  currentDirection: RepositoryWorkItemDirection;
  currentSort: RepositoryWorkItemSort;
  navigate: NavigateFn;
}) {
  const ref = useRef<HTMLDetailsElement>(null);
  useCloseOnOutsideClickAndEscape(ref);
  const sortLabel =
    props.currentSort === "created"
      ? props.copy.sortCreated
      : props.currentSort === "comments"
        ? props.copy.sortComments
        : props.copy.sortUpdated;

  function select(changes: Partial<RepositoryMergeRequestQuery>) {
    props.navigate(changes);
    closeDetails(ref);
  }

  return (
    <details className={styles.dropdown} ref={ref}>
      <summary className={styles.dropdownTrigger}>
        <span className={styles.dropdownTriggerLabel}>{sortLabel}</span>
        <ChevronDown aria-hidden="true" size={14} />
      </summary>
      <div className={styles.dropdownMenu}>
        <SortOption copy={props.copy.sortUpdated} current={props.currentSort} onSelect={select} sort="updated" />
        <SortOption copy={props.copy.sortCreated} current={props.currentSort} onSelect={select} sort="created" />
        <SortOption copy={props.copy.sortComments} current={props.currentSort} onSelect={select} sort="comments" />
        <div className={styles.dropdownDivider} />
        <SortOption
          copy={props.copy.descending}
          current={props.currentDirection}
          onSelect={select}
          sort="direction-desc"
        />
        <SortOption
          copy={props.copy.ascending}
          current={props.currentDirection}
          onSelect={select}
          sort="direction-asc"
        />
      </div>
    </details>
  );
}

function SortOption(props: {
  copy: string;
  current: string;
  onSelect: (changes: Partial<RepositoryMergeRequestQuery>) => void;
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
        {isActive && <Check size={14} />}
      </span>
    </button>
  );
}

function closeDetails(ref: RefObject<HTMLDetailsElement | null>) {
  if (ref.current) ref.current.open = false;
}

function useCloseOnOutsideClickAndEscape(ref: RefObject<HTMLDetailsElement | null>) {
  useEffect(() => {
    function handleClick(event: MouseEvent) {
      const el = ref.current;
      if (!el || !el.open) return;
      if (!el.contains(event.target as Node)) closeDetails(ref);
    }
    function handleKey(event: KeyboardEvent) {
      if (event.key === "Escape" && ref.current?.open) closeDetails(ref);
    }
    document.addEventListener("click", handleClick);
    document.addEventListener("keydown", handleKey);
    return () => {
      document.removeEventListener("click", handleClick);
      document.removeEventListener("keydown", handleKey);
    };
  }, [ref]);
}
