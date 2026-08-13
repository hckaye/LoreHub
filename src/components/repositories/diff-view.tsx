import { ChevronDown } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { LoreDiff, LoreDiffFile } from "@/lib/api-types";
import { parseReviewDiff, type ReviewDiffRow } from "@/lib/review-diff";

import styles from "./diff-view.module.css";

type DiffViewProps = { diff: LoreDiff; dictionary: Dictionary; linkPrefix?: string };

type DiffFile = { file: LoreDiffFile; rows: ReviewDiffRow[]; additions: number; deletions: number };

export function DiffView({ diff, dictionary, linkPrefix }: DiffViewProps) {
  if (diff.files.length === 0) {
    return <p className={styles.empty}>{dictionary.pullRequestDetail.noChangedFiles}</p>;
  }
  const files = diff.files.map(describeFile);
  const additions = files.reduce((total, entry) => total + entry.additions, 0);
  const deletions = files.reduce((total, entry) => total + entry.deletions, 0);
  return (
    <div className={styles.diff}>
      <p className={styles.summary}>
        <strong>{dictionary.commitHistory.changedFileCount.replace("{count}", String(files.length))}</strong>
        <DiffCounts additions={additions} deletions={deletions} dictionary={dictionary} />
      </p>
      {files.map((entry) => (
        <DiffFileCard dictionary={dictionary} entry={entry} key={entry.file.path} linkPrefix={linkPrefix} />
      ))}
      {(diff.hasMore || diff.truncated) && <p className={styles.note}>{dictionary.codeBrowser.diffTruncated}</p>}
    </div>
  );
}

function DiffFileCard({
  dictionary,
  entry,
  linkPrefix,
}: {
  dictionary: Dictionary;
  entry: DiffFile;
  linkPrefix?: string;
}) {
  const { file } = entry;
  return (
    <details className={styles.file} open>
      <summary className={styles.fileHeader}>
        <ChevronDown aria-hidden="true" className={styles.chevron} size={16} />
        <span className={styles.filePath}>
          {linkPrefix ? <Link href={`${linkPrefix}${encodeURIComponent(file.path)}`}>{file.path}</Link> : file.path}
        </span>
        <span className={styles.fileAction}>{actionLabel(file.action, dictionary)}</span>
        <DiffCounts additions={entry.additions} deletions={entry.deletions} dictionary={dictionary} />
      </summary>
      {file.binary ? (
        <p className={styles.note}>{dictionary.codeBrowser.binary}</p>
      ) : entry.rows.length === 0 ? (
        <p className={styles.note}>{dictionary.commitHistory.emptyDiff}</p>
      ) : (
        <table className={styles.lines}>
          <tbody>
            {entry.rows.map((row) => (
              <DiffLine key={row.key} row={row} />
            ))}
          </tbody>
        </table>
      )}
      {file.truncated && <p className={styles.note}>{dictionary.codeBrowser.diffTruncated}</p>}
    </details>
  );
}

function DiffLine({ row }: { row: ReviewDiffRow }) {
  if (row.kind === "header") {
    return (
      <tr className={styles.hunk}>
        <td colSpan={3}>{row.content}</td>
      </tr>
    );
  }
  return (
    <tr className={styles.line} data-kind={row.kind}>
      <td className={styles.number}>{row.oldLine}</td>
      <td className={styles.number}>{row.newLine}</td>
      <td className={styles.code} data-marker={marker(row.kind)}>
        <code>{row.content || " "}</code>
      </td>
    </tr>
  );
}

function DiffCounts({
  additions,
  deletions,
  dictionary,
}: {
  additions: number;
  deletions: number;
  dictionary: Dictionary;
}) {
  const copy = dictionary.commitHistory;
  return (
    <span className={styles.counts}>
      <span aria-label={copy.additions.replace("{count}", String(additions))} className={styles.additions}>
        +{additions}
      </span>
      <span aria-label={copy.deletions.replace("{count}", String(deletions))} className={styles.deletions}>
        −{deletions}
      </span>
    </span>
  );
}

function describeFile(file: LoreDiffFile): DiffFile {
  const rows = file.binary ? [] : parseReviewDiff(file);
  return {
    file,
    rows,
    additions: rows.filter((row) => row.kind === "added").length,
    deletions: rows.filter((row) => row.kind === "deleted").length,
  };
}

function marker(kind: ReviewDiffRow["kind"]): string {
  if (kind === "added") return "+";
  return kind === "deleted" ? "−" : " ";
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
