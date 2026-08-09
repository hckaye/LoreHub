"use client";

import { GitBranch } from "lucide-react";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Branch } from "@/lib/api-types";

import styles from "./branch-selector.module.css";

type BranchSelectorProps = {
  branches: Branch[];
  defaultBranch: string;
  dictionary: Dictionary;
};

export function BranchSelector({ branches, defaultBranch, dictionary }: BranchSelectorProps) {
  const initial = branches.find((branch) => branch.name === defaultBranch) ?? branches[0];
  const [selectedName, setSelectedName] = useState(initial?.name ?? "");
  const selected = branches.find((branch) => branch.name === selectedName);
  if (branches.length === 0) {
    return null;
  }
  return (
    <div className={styles.wrapper}>
      <label htmlFor="branch-selector">
        <GitBranch aria-hidden="true" size={16} />
        {dictionary.repository.branchSelector}
      </label>
      <select id="branch-selector" onChange={(event) => setSelectedName(event.target.value)} value={selectedName}>
        {branches.map((branch) => (
          <option disabled={branch.archived} key={branch.id} value={branch.name}>
            {branch.name}
            {branch.name === defaultBranch ? ` · ${dictionary.repository.defaultBranch}` : ""}
          </option>
        ))}
      </select>
      {selected && (
        <p>
          <span>{dictionary.repository.selectedBranch}:</span> <strong>{selected.name}</strong>
          <code title={selected.latestRevision}>{shortRevision(selected.latestRevision)}</code>
        </p>
      )}
    </div>
  );
}

function shortRevision(revision: string): string {
  return revision.length > 12 ? revision.slice(0, 12) : revision;
}
