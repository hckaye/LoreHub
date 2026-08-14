import Link from "next/link";

import { CopyButton } from "@/components/ui/copy-button";
import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { LoreRevision } from "@/lib/api-types";
import { formatRelativeTime, formatTimestamp, isUsableTimestamp, shortRevision } from "@/lib/format";
import { revisionBody, revisionSubject } from "@/lib/revision-history";

import styles from "./commit-detail.module.css";

type CommitHeaderProps = {
  dictionary: Dictionary;
  locale: Locale;
  owner: string;
  repository: string;
  revision: LoreRevision;
};

export function CommitHeader({ dictionary, locale, owner, repository, revision }: CommitHeaderProps) {
  const copy = dictionary.commitHistory;
  const subject = revisionSubject(revision.message) || shortRevision(revision.revision);
  const body = revisionBody(revision.message);
  const createdAt = revision.createdAt;
  const hasCreatedAt = isUsableTimestamp(createdAt);
  const relative = hasCreatedAt ? formatRelativeTime(createdAt, locale) : "";
  return (
    <header className={styles.header}>
      <h1 className={styles.title}>{subject}</h1>
      {body && <pre className={styles.body}>{body}</pre>}
      <div className={styles.metaBar}>
        <span className={styles.author}>
          <strong>{revision.author || copy.unknownAuthor}</strong>
          {hasCreatedAt ? (
            <span title={formatTimestamp(createdAt, locale)}>{copy.committed.replace("{time}", relative).trim()}</span>
          ) : null}
        </span>
        <span className={styles.identifiers}>
          {revision.parents.length > 0 && (
            <span className={styles.parents}>
              {copy.parentRevisions}
              {revision.parents.map((parent) => (
                <Link href={commitHref(locale, owner, repository, parent)} key={parent} title={parent}>
                  {shortRevision(parent)}
                </Link>
              ))}
            </span>
          )}
          <span className={styles.revision}>
            {dictionary.codeBrowser.revision}
            <code title={revision.revision}>{shortRevision(revision.revision)}</code>
            <CopyButton copiedLabel={copy.revisionCopied} label={copy.copyRevision} value={revision.revision} />
          </span>
        </span>
      </div>
    </header>
  );
}

function commitHref(locale: Locale, owner: string, repository: string, revision: string): string {
  const path = `/${locale}/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/commit`;
  return `${path}?revision=${encodeURIComponent(revision)}`;
}
