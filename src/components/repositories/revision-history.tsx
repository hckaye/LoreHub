import { GitCommitHorizontal } from "lucide-react";
import Link from "next/link";

import { CopyButton } from "@/components/ui/copy-button";
import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import { formatRelativeTime, formatTimestamp, shortRevision } from "@/lib/format";
import {
  groupRevisionRows,
  revisionHistoryPageHref,
  revisionSubject,
  type RevisionHistoryQuery,
  type RevisionRow,
} from "@/lib/revision-history";

import styles from "./revision-history.module.css";

type RevisionHistoryProps = {
  basePath: string;
  dictionary: Dictionary;
  hasNext: boolean;
  locale: Locale;
  owner: string;
  page: number;
  query: RevisionHistoryQuery;
  repository: string;
  rows: RevisionRow[];
};

export function RevisionHistory(props: RevisionHistoryProps) {
  const copy = props.dictionary.commitHistory;
  if (props.rows.length === 0) {
    return <p className={styles.empty}>{props.dictionary.codeBrowser.emptyHistory}</p>;
  }
  return (
    <div className={styles.history}>
      {groupRevisionRows(props.rows, props.locale).map((group) => (
        <section className={styles.group} key={group.key || "undated"}>
          <h3 className={styles.groupHeading}>
            <GitCommitHorizontal aria-hidden="true" size={16} />
            {group.date ? copy.commitsOn.replace("{date}", group.date) : copy.undatedCommits}
          </h3>
          <ol className={styles.rows}>
            {group.rows.map((row) => (
              <RevisionRowItem key={row.revision} row={row} {...props} />
            ))}
          </ol>
        </section>
      ))}
      <HistoryPagination {...props} />
    </div>
  );
}

function RevisionRowItem({ row, ...props }: RevisionHistoryProps & { row: RevisionRow }) {
  const copy = props.dictionary.commitHistory;
  const subject = revisionSubject(row.message) || shortRevision(row.revision);
  return (
    <li className={styles.row}>
      <div className={styles.details}>
        <Link className={styles.message} href={commitHref(props, row.revision)}>
          {subject}
        </Link>
        <p className={styles.byline}>
          <strong>{row.author || copy.unknownAuthor}</strong>
          <span title={row.createdAt ? formatTimestamp(row.createdAt, props.locale) : undefined}>
            {committedLabel(row, props.locale, copy.committed)}
          </span>
        </p>
      </div>
      <div className={styles.revision}>
        <Link className={styles.revisionLink} href={commitHref(props, row.revision)} title={row.revision}>
          {shortRevision(row.revision)}
        </Link>
        <CopyButton copiedLabel={copy.revisionCopied} label={copy.copyRevision} value={row.revision} />
      </div>
    </li>
  );
}

function HistoryPagination(props: RevisionHistoryProps) {
  const copy = props.dictionary.commitHistory;
  if (props.page === 1 && !props.hasNext) {
    return null;
  }
  return (
    <nav aria-label={copy.historyPages} className={styles.pagination}>
      {props.page > 1 ? (
        <Link href={revisionHistoryPageHref(props.basePath, props.query, props.page - 1)}>{copy.newerCommits}</Link>
      ) : (
        <span aria-disabled="true">{copy.newerCommits}</span>
      )}
      {props.hasNext ? (
        <Link href={revisionHistoryPageHref(props.basePath, props.query, props.page + 1)}>{copy.olderCommits}</Link>
      ) : (
        <span aria-disabled="true">{copy.olderCommits}</span>
      )}
    </nav>
  );
}

function committedLabel(row: RevisionRow, locale: Locale, template: string): string {
  const relative = row.createdAt ? formatRelativeTime(row.createdAt, locale) : "";
  return relative ? template.replace("{time}", relative) : template.replace("{time}", "").trim();
}

function commitHref(props: RevisionHistoryProps, revision: string): string {
  const path = `/${props.locale}/${encodeURIComponent(props.owner)}/${encodeURIComponent(props.repository)}/commit`;
  return `${path}?revision=${encodeURIComponent(revision)}`;
}
