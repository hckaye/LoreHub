import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { RevisionHistoryEntry } from "@/lib/api-types";

import styles from "./code-detail.module.css";

type RevisionHistoryProps = {
  entries: RevisionHistoryEntry[];
  locale: "en" | "ja";
  owner: string;
  repository: string;
  dictionary: Dictionary;
};

export function RevisionHistory({ entries, locale, owner, repository, dictionary }: RevisionHistoryProps) {
  if (entries.length === 0) {
    return <p className={styles.meta}>{dictionary.codeBrowser.emptyHistory}</p>;
  }
  return (
    <div className={styles.tableWrap}>
      <table className={styles.table}>
        <thead>
          <tr>
            <th scope="col">{dictionary.codeBrowser.revision}</th>
            <th scope="col">{dictionary.pullRequestDetail.commits}</th>
          </tr>
        </thead>
        <tbody>
          {entries.map((entry) => (
            <tr key={entry.revision}>
              <td>
                <Link href={revisionHref(locale, owner, repository, entry.revision)}>{entry.revision}</Link>
              </td>
              <td>{entry.number}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function revisionHref(locale: "en" | "ja", owner: string, repository: string, revision: string): string {
  const path = `/${locale}/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/commit`;
  return `${path}?revision=${encodeURIComponent(revision)}`;
}
