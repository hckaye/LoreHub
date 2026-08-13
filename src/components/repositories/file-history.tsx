import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { FileHistoryEntry } from "@/lib/api-types";
import { shortRevision } from "@/lib/format";

import styles from "./blob-view.module.css";

type FileHistoryProps = {
  entries: FileHistoryEntry[];
  locale: Locale;
  owner: string;
  repository: string;
  dictionary: Dictionary;
};

export function FileHistory({ entries, locale, owner, repository, dictionary }: FileHistoryProps) {
  const copy = dictionary.codeBrowser;
  if (entries.length === 0) {
    return <p className={styles.historyEmpty}>{copy.emptyHistory}</p>;
  }
  return (
    <div className={styles.historyCard}>
      <h3 className={styles.historyHeading}>{copy.fileHistory}</h3>
      <ul className={styles.historyList}>
        {entries.map((entry) => (
          <li className={styles.historyRow} key={`${entry.revision}-${entry.path}`}>
            <span className={styles.historySha}>
              <Link href={revisionHref(locale, owner, repository, entry.revision)}>
                {shortRevision(entry.revision)}
              </Link>
            </span>
            <span className={styles.historyAction}>{actionLabel(entry.action, dictionary)}</span>
            <span className={styles.historySize}>
              {entry.size.toLocaleString()} {copy.bytes}
            </span>
          </li>
        ))}
      </ul>
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

function revisionHref(locale: Locale, owner: string, repository: string, revision: string): string {
  const path = `/${locale}/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/commit`;
  return `${path}?revision=${encodeURIComponent(revision)}`;
}
