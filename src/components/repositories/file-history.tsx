import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { FileHistoryEntry } from "@/lib/api-types";

import styles from "./code-detail.module.css";

type FileHistoryProps = {
  entries: FileHistoryEntry[];
  locale: "en" | "ja";
  owner: string;
  repository: string;
  dictionary: Dictionary;
};

export function FileHistory({ entries, locale, owner, repository, dictionary }: FileHistoryProps) {
  if (entries.length === 0) {
    return <p className={styles.meta}>{dictionary.codeBrowser.emptyHistory}</p>;
  }
  return (
    <div className={styles.tableWrap}>
      <table className={styles.table}>
        <thead>
          <tr>
            <th scope="col">{dictionary.codeBrowser.revision}</th>
            <th scope="col">{dictionary.codeBrowser.action}</th>
            <th scope="col">{dictionary.codeBrowser.size}</th>
          </tr>
        </thead>
        <tbody>
          {entries.map((entry) => (
            <tr key={`${entry.revision}-${entry.path}`}>
              <td>
                <Link href={revisionHref(locale, owner, repository, entry.revision)}>{entry.revision}</Link>
              </td>
              <td>{actionLabel(entry.action, dictionary)}</td>
              <td>
                {entry.size.toLocaleString()} {dictionary.codeBrowser.bytes}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function actionLabel(action: string, dictionary: Dictionary): string {
  switch (action) {
    case "added":
      return dictionary.codeBrowser.actions.added;
    case "deleted":
      return dictionary.codeBrowser.actions.deleted;
    case "moved":
      return dictionary.codeBrowser.actions.moved;
    case "copied":
      return dictionary.codeBrowser.actions.copied;
    default:
      return dictionary.codeBrowser.actions.modified;
  }
}

function revisionHref(locale: "en" | "ja", owner: string, repository: string, revision: string): string {
  const path = `/${locale}/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/commit`;
  return `${path}?revision=${encodeURIComponent(revision)}`;
}
