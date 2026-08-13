import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { FileHistoryEntry, LoreFile, TreeEntry } from "@/lib/api-types";
import { repositoryPath } from "@/lib/routes";

import { BlobViewFile } from "./blob-view-file";
import { FileHistory } from "./file-history";
import styles from "./blob-view.module.css";

type BlobViewProps = {
  dictionary: Dictionary;
  locale: Locale;
  owner: string;
  repository: string;
  repositoryName: string;
  branch: string | undefined;
  file: LoreFile;
  entries: TreeEntry[];
  history: FileHistoryEntry[];
};

export function BlobView(props: BlobViewProps) {
  const copy = props.dictionary.codeBrowser;
  const base = repositoryPath(props.locale, props.owner, props.repository);
  const segments = props.file.path.split("/").filter(Boolean);
  const fileName = segments[segments.length - 1] ?? props.file.path;
  const isMarkdown = /\.(md|markdown)$/i.test(props.file.path);
  const lineCount = props.file.content ? props.file.content.split("\n").length : 0;
  const rawQuery = new URLSearchParams({ revision: props.file.revision, path: props.file.path }).toString();
  const rawPath = `/api/v1/repositories/${encodeURIComponent(props.owner)}/${encodeURIComponent(
    props.repository,
  )}/raw?${rawQuery}`;

  return (
    <section aria-labelledby="blob-view-title" className={styles.browser}>
      <h2 className="visually-hidden" id="blob-view-title">
        {copy.blobTitle}
      </h2>
      <div className={styles.fileCard}>
        <nav aria-label={copy.breadcrumbLabel} className={styles.breadcrumbBar}>
          <ol className={styles.breadcrumb}>
            <li>
              <Link href={treeHref(base, props.branch, props.file.revision, "")}>{props.repositoryName}</Link>
            </li>
            {segments.slice(0, -1).map((part, index) => (
              <li key={`${part}-${index}`}>
                <span aria-hidden="true">/</span>
                <Link href={treeHref(base, props.branch, props.file.revision, segments.slice(0, index + 1).join("/"))}>
                  {part}
                </Link>
              </li>
            ))}
            <li>
              <span aria-hidden="true">/</span>
              <span className={styles.current}>{fileName}</span>
            </li>
          </ol>
        </nav>
      </div>
      <BlobViewFile
        binary={props.file.binary}
        content={props.file.content}
        dictionary={props.dictionary}
        entries={props.entries}
        fileName={fileName}
        isMarkdown={isMarkdown}
        lineCount={lineCount}
        locale={props.locale}
        owner={props.owner}
        rawPath={rawPath}
        readmePath={props.file.path}
        repository={props.repository}
        revision={props.file.revision}
        size={props.file.size}
        truncated={props.file.truncated}
      />
      {props.history.length > 0 && (
        <FileHistory
          dictionary={props.dictionary}
          entries={props.history}
          locale={props.locale}
          owner={props.owner}
          repository={props.repository}
        />
      )}
    </section>
  );
}

function treeHref(base: string, branch: string | undefined, revision: string, path: string): string {
  const params = new URLSearchParams();
  if (branch) {
    params.set("branch", branch);
  } else {
    params.set("revision", revision);
  }
  if (path) {
    params.set("path", path);
  }
  return `${base}?${params.toString()}`;
}
