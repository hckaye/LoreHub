import { BookOpen, ServerOff } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { Repository } from "@/lib/api-types";
import { repositoryPath } from "@/lib/routes";

import { EmptyState } from "../ui/empty-state";
import styles from "./repository-chooser.module.css";

type RepositoryChooserProps = {
  locale: Locale;
  dictionary: Dictionary;
  repositories: Repository[] | null;
  unavailable: boolean;
  section: "issues" | "pulls";
};

export function RepositoryChooser({ locale, dictionary, repositories, unavailable, section }: RepositoryChooserProps) {
  if (unavailable) {
    return (
      <EmptyState
        body={dictionary.home.apiUnavailableBody}
        icon={<ServerOff aria-hidden="true" />}
        title={dictionary.home.apiUnavailableTitle}
        tone="warning"
      />
    );
  }
  const writableRepositories = repositories?.filter((repository) => !repository.archivedAt);
  if (!writableRepositories || writableRepositories.length === 0) {
    return (
      <EmptyState
        body={dictionary.forms.chooseRepositoryBody}
        icon={<BookOpen aria-hidden="true" />}
        title={dictionary.forms.chooseRepositoryEmpty}
      />
    );
  }
  return (
    <div className={styles.list}>
      {writableRepositories.map((repository) => {
        const href = `${repositoryPath(locale, repository.owner, repository.slug, section)}/new`;
        return (
          <Link className={styles.item} href={href} key={repository.id}>
            <span>
              <strong>
                {repository.owner}/{repository.slug}
              </strong>
              <small>{repository.displayName}</small>
            </span>
            <span className={styles.arrow} aria-hidden="true">
              →
            </span>
          </Link>
        );
      })}
    </div>
  );
}
