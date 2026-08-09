import { GitBranch } from "lucide-react";

import type { Dictionary } from "@/i18n";
import type { Branch } from "@/lib/api-types";

import { EmptyState } from "../ui/empty-state";
import styles from "./branch-list.module.css";

type BranchListProps = {
  branches: Branch[];
  dictionary: Dictionary;
};

export function BranchList({ branches, dictionary }: BranchListProps) {
  if (branches.length === 0) {
    return (
      <EmptyState
        icon={<GitBranch aria-hidden="true" />}
        title={dictionary.repository.noBranches}
        body={dictionary.repository.branchesDescription}
      />
    );
  }

  return (
    <div className={styles.list}>
      {branches.map((branch) => (
        <div className={styles.row} key={branch.id}>
          <GitBranch aria-hidden="true" size={17} />
          <div className={styles.name}>
            <strong>{branch.name}</strong>
            {branch.category && <span>{branch.category}</span>}
          </div>
          <code title={branch.latestRevision}>{shortRevision(branch.latestRevision)}</code>
        </div>
      ))}
    </div>
  );
}

function shortRevision(revision: string): string {
  return revision.length > 12 ? revision.slice(0, 12) : revision;
}
