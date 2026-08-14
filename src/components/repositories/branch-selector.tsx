"use client";

import { ChevronDown, GitBranch, Tag } from "lucide-react";
import { useMemo, useState } from "react";

import { PopupMenu } from "@/components/ui/popup-menu";
import type { Dictionary } from "@/i18n";
import type { Branch, RepositoryTag } from "@/lib/api-types";

import styles from "./branch-selector.module.css";

export type BranchSelection = { kind: "branch"; name: string } | { kind: "tag"; name: string; revision: string };

type BranchSelectorProps = {
  branches: Branch[];
  tags: RepositoryTag[];
  selectedKind: BranchSelection["kind"];
  selectedName: string;
  dictionary: Dictionary;
  onSelect: (selection: BranchSelection) => void;
};

export function BranchSelector({
  branches,
  tags,
  selectedKind,
  selectedName,
  dictionary,
  onSelect,
}: BranchSelectorProps) {
  const [filter, setFilter] = useState("");
  const [tab, setTab] = useState<"branches" | "tags">(selectedKind === "tag" ? "tags" : "branches");
  const normalizedFilter = filter.trim().toLocaleLowerCase();
  const filteredBranches = useMemo(
    () => branches.filter((branch) => matchesFilter(branch.name, normalizedFilter)),
    [branches, normalizedFilter],
  );
  const filteredTags = useMemo(
    () => tags.filter((tag) => matchesFilter(tag.name, normalizedFilter)),
    [tags, normalizedFilter],
  );
  const selectedIcon =
    selectedKind === "tag" ? <Tag aria-hidden="true" size={16} /> : <GitBranch aria-hidden="true" size={16} />;
  const selectedLabel = selectedName || dictionary.repository.chooseBranch;

  function select(selection: BranchSelection, close: () => void) {
    onSelect(selection);
    setFilter("");
    close();
  }

  return (
    <PopupMenu
      align="start"
      className={styles.popover}
      panelClassName={styles.menu}
      panelRole="none"
      trigger={
        <>
          {selectedIcon}
          <span>{selectedLabel}</span>
          <ChevronDown aria-hidden="true" size={15} />
        </>
      }
      triggerClassName={styles.summary}
      triggerProps={{ "aria-label": dictionary.codeBrowser.selectReference }}
    >
      {(close) => (
        <>
          <label className="visually-hidden" htmlFor="branch-selector-filter">
            {dictionary.codeBrowser.branchFilter}
          </label>
          <input
            autoComplete="off"
            id="branch-selector-filter"
            onChange={(event) => setFilter(event.target.value)}
            placeholder={dictionary.codeBrowser.branchFilter}
            type="search"
            value={filter}
          />
          <div aria-label={dictionary.codeBrowser.selectReference} className={styles.tabs} role="tablist">
            <button
              aria-selected={tab === "branches"}
              className={tab === "branches" ? styles.activeTab : ""}
              onClick={() => setTab("branches")}
              role="tab"
              type="button"
            >
              <GitBranch aria-hidden="true" size={14} />
              {dictionary.codeBrowser.branches}
            </button>
            <button
              aria-selected={tab === "tags"}
              className={tab === "tags" ? styles.activeTab : ""}
              onClick={() => setTab("tags")}
              role="tab"
              type="button"
            >
              <Tag aria-hidden="true" size={14} />
              {dictionary.codeBrowser.tags}
            </button>
          </div>
          <div aria-label={dictionary.codeBrowser.selectReference} className={styles.options} role="listbox">
            {tab === "branches"
              ? filteredBranches.map((branch) => (
                  <button
                    aria-selected={selectedKind === "branch" && selectedName === branch.name}
                    className={styles.option}
                    key={branch.id}
                    onClick={() => select({ kind: "branch", name: branch.name }, close)}
                    role="option"
                    type="button"
                  >
                    <GitBranch aria-hidden="true" size={15} />
                    <span className={styles.optionName}>{branch.name}</span>
                    {branch.current && <span className={styles.badge}>{dictionary.repository.branchCurrent}</span>}
                    {branch.archived && <span className={styles.badge}>{dictionary.repository.branchArchived}</span>}
                  </button>
                ))
              : filteredTags.map((tag) => (
                  <button
                    aria-selected={selectedKind === "tag" && selectedName === tag.name}
                    className={styles.option}
                    key={`${tag.name}-${tag.revision}`}
                    onClick={() => select({ kind: "tag", name: tag.name, revision: tag.revision }, close)}
                    role="option"
                    type="button"
                  >
                    <Tag aria-hidden="true" size={15} />
                    <span className={styles.optionName}>{tag.name}</span>
                  </button>
                ))}
            {tab === "branches" && filteredBranches.length === 0 && (
              <p className={styles.empty}>
                {normalizedFilter
                  ? dictionary.codeBrowser.noMatchingReferences
                  : dictionary.codeBrowser.noBranchesFound}
              </p>
            )}
            {tab === "tags" && filteredTags.length === 0 && (
              <p className={styles.empty}>
                {normalizedFilter ? dictionary.codeBrowser.noMatchingReferences : dictionary.codeBrowser.noTagsFound}
              </p>
            )}
          </div>
        </>
      )}
    </PopupMenu>
  );
}

function matchesFilter(value: string, filter: string): boolean {
  return filter === "" || value.toLocaleLowerCase().includes(filter);
}
