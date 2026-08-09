import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { LoreDiff } from "@/lib/api-types";

import styles from "./code-detail.module.css";

type DiffViewProps = { diff: LoreDiff; dictionary: Dictionary; linkPrefix?: string };

export function DiffView({ diff, dictionary, linkPrefix }: DiffViewProps) {
  if (diff.files.length === 0) {
    return <p className={styles.meta}>{dictionary.pullRequestDetail.noChangedFiles}</p>;
  }
  return (
    <div className={styles.panel}>
      {diff.files.map((file) => (
        <section className={styles.status} key={file.path}>
          <div className={styles.heading}>
            <div>
              <h3>
                {linkPrefix ? (
                  <Link href={`${linkPrefix}${encodeURIComponent(file.path)}`}>{file.path}</Link>
                ) : (
                  file.path
                )}
              </h3>
              <p>{actionLabel(file.action, dictionary)}</p>
            </div>
            {file.binary && <span>{dictionary.codeBrowser.binary}</span>}
          </div>
          {!file.binary && file.patch && <pre className={styles.patch}>{file.patch}</pre>}
          {file.truncated && <p className={styles.meta}>{dictionary.codeBrowser.diffTruncated}</p>}
        </section>
      ))}
      {(diff.hasMore || diff.truncated) && <p className={styles.meta}>{dictionary.codeBrowser.diffTruncated}</p>}
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
