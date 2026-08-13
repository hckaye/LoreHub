"use client";

import { Check, ChevronDown, CircleDot, Search } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";

import type { Dictionary } from "@/i18n";
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
      <div className={styles.stateToggle}>
        <button
          aria-current={state === "open" ? "true" : undefined}
          className={state === "open" ? styles.stateActive : styles.stateLink}
          onClick={() => navigate({ state: "open" })}
          type="button"
        >
          <CircleDot aria-hidden="true" size={16} />
          {copy.openWithCount.replace("{count}", String(openCount))}
        </button>
        <button
          aria-current={state === "closed" ? "true" : undefined}
          className={state === "closed" ? styles.stateActive : styles.stateLink}
          onClick={() => navigate({ state: "closed" })}
          type="button"
        >
          <Check aria-hidden="true" size={16} />
          {copy.closedWithCount.replace("{count}", String(closedCount))}
        </button>
        {isPulls && (
          <button
            aria-current={state === "merged" ? "true" : undefined}
            className={state === "merged" ? styles.stateActive : styles.stateLink}
            onClick={() => navigate({ state: "merged" })}
            type="button"
          >
            <Check aria-hidden="true" size={16} />
            {copy.mergedWithCount.replace("{count}", String(mergedCount))}
          </button>
        )}
      </div>

      <SearchForm basePath={props.basePath} copy={copy} defaultValue={props.query.q} navigate={navigate} />

      <div className={styles.dropdowns}>
        <FilterDropdown
          copy={copy.author}
          currentValue={props.query.author}
          label={copy.author}
          navigate={navigate}
          paramName="author"
          placeholder={copy.authorPlaceholder}
        />
        <FilterDropdown
          copy={copy.assignee}
          currentValue={props.query.assignee}
          label={copy.assignee}
          navigate={navigate}
          paramName="assignee"
          placeholder={copy.assigneePlaceholder}
        />
        <FilterDropdown
          copy={copy.label}
          currentValue={props.query.labels?.join(", ")}
          label={copy.label}
          navigate={navigate}
          paramName="label"
          placeholder={copy.labelPlaceholder}
        />
        <FilterDropdown
          copy={copy.milestone}
          currentValue={props.query.milestone}
          label={copy.milestone}
          navigate={navigate}
          paramName="milestone"
          placeholder={copy.milestonePlaceholder}
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

function FilterDropdown(props: {
  copy: string;
  currentValue?: string;
  label: string;
  navigate: NavigateFn;
  paramName: "author" | "assignee" | "label" | "milestone";
  placeholder: string;
}) {
  const ref = useRef<HTMLDetailsElement>(null);
  const [value, setValue] = useState(props.currentValue ?? "");
  useCloseOnOutsideClickAndEscape(ref);
  const displayValue = props.currentValue ? props.currentValue : props.label;
  return (
    <details className={styles.dropdown} ref={ref}>
      <summary className={styles.dropdownTrigger}>
        <span>{displayValue}</span>
        <ChevronDown aria-hidden="true" size={14} />
      </summary>
      <div className={styles.dropdownMenu}>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            const trimmed = value.trim();
            if (props.paramName === "label") {
              const labels = trimmed
                ? trimmed
                    .split(",")
                    .map((l) => l.trim())
                    .filter(Boolean)
                : [];
              props.navigate({ labels: labels.length > 0 ? labels : undefined, page: 1 });
              if (ref.current) ref.current.open = false;
              return;
            }
            props.navigate({
              [props.paramName]: trimmed || undefined,
              page: 1,
            } as Partial<RepositoryMergeRequestQuery>);
            if (ref.current) ref.current.open = false;
          }}
        >
          <input
            autoFocus
            className={styles.dropdownInput}
            maxLength={props.paramName === "milestone" ? 30 : 100}
            onChange={(event) => setValue(event.target.value)}
            placeholder={props.placeholder}
            type="text"
            value={value}
          />
          <button className={styles.dropdownSubmit} type="submit">
            {props.copy}
          </button>
        </form>
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
  return (
    <details className={styles.dropdown} ref={ref}>
      <summary className={styles.dropdownTrigger}>
        <span>{sortLabel}</span>
        <ChevronDown aria-hidden="true" size={14} />
      </summary>
      <div className={styles.dropdownMenu}>
        <SortOption
          copy={props.copy.sortUpdated}
          current={props.currentSort}
          navigate={props.navigate}
          sort="updated"
        />
        <SortOption
          copy={props.copy.sortCreated}
          current={props.currentSort}
          navigate={props.navigate}
          sort="created"
        />
        <SortOption
          copy={props.copy.sortComments}
          current={props.currentSort}
          navigate={props.navigate}
          sort="comments"
        />
        <div className={styles.dropdownDivider} />
        <SortOption
          copy={props.copy.descending}
          current={props.currentDirection}
          navigate={props.navigate}
          sort="direction-desc"
        />
        <SortOption
          copy={props.copy.ascending}
          current={props.currentDirection}
          navigate={props.navigate}
          sort="direction-asc"
        />
      </div>
    </details>
  );
}

function SortOption(props: {
  copy: string;
  current: string;
  navigate: NavigateFn;
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
          props.navigate({ direction, page: 1 });
        } else {
          props.navigate({ sort: props.sort as RepositoryWorkItemSort, page: 1 });
        }
      }}
      type="button"
    >
      {isActive && <Check aria-hidden="true" size={14} />}
      {props.copy}
    </button>
  );
}

function useCloseOnOutsideClickAndEscape(ref: React.RefObject<HTMLDetailsElement | null>) {
  useEffect(() => {
    function handleClick(event: MouseEvent) {
      const el = ref.current;
      if (!el || !el.open) return;
      if (!el.contains(event.target as Node)) el.open = false;
    }
    function handleKey(event: KeyboardEvent) {
      if (event.key === "Escape" && ref.current?.open) ref.current.open = false;
    }
    document.addEventListener("click", handleClick);
    document.addEventListener("keydown", handleKey);
    return () => {
      document.removeEventListener("click", handleClick);
      document.removeEventListener("keydown", handleKey);
    };
  }, [ref]);
}
