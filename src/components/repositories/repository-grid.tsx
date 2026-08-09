import { PackageOpen } from "lucide-react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { Repository } from "@/lib/api-types";

import { EmptyState } from "../ui/empty-state";
import { RepositoryCard } from "./repository-card";
import styles from "./repository-grid.module.css";

type RepositoryGridProps = {
  repositories: Repository[];
  locale: Locale;
  dictionary: Dictionary;
  emptyTitle: string;
  emptyBody: string;
};

export function RepositoryGrid(props: RepositoryGridProps) {
  if (props.repositories.length === 0) {
    return <EmptyState icon={<PackageOpen aria-hidden="true" />} title={props.emptyTitle} body={props.emptyBody} />;
  }

  return (
    <div className={styles.grid}>
      {props.repositories.map((repository) => (
        <RepositoryCard
          key={repository.id}
          repository={repository}
          locale={props.locale}
          dictionary={props.dictionary}
        />
      ))}
    </div>
  );
}
