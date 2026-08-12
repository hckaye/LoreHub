"use client";

import { FileCode2, Folder, GitBranch, GitCommitHorizontal, History, Link2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";

import type { Dictionary } from "@/i18n";
import type { Branch, LoreTree } from "@/lib/api-types";
import { repositoryBranchesPath, repositoryPath } from "@/lib/routes";

import styles from "./code-browser.module.css";

type CodeBrowserProps = {
  locale: "en" | "ja";
  owner: string;
  repository: string;
  branch: string;
  branches: Branch[];
  tree: LoreTree;
  parentRevision?: string;
  dictionary: Dictionary;
};

export function CodeBrowser({
  locale,
  owner,
  repository,
  branch,
  branches,
  tree,
  parentRevision,
  dictionary,
}: CodeBrowserProps) {
  const basePath = repositoryPath(locale, owner, repository);
  const router = useRouter();
  const commitsPath = `${basePath}/commits?branch=${encodeURIComponent(branch)}`;
  const parentPath = parentTreePath(tree.path);
  return (
    <section aria-labelledby="code-browser-title" className={styles.browser}>
      <div className={styles.toolbar}>
        <div className={styles.branchControls}>
          <div className={styles.branchBox}>
            <GitBranch aria-hidden="true" size={16} />
            <label className="visually-hidden" htmlFor="code-branch">
              {dictionary.repository.branchSelector}
            </label>
            <select
              aria-label={dictionary.repository.branchSelector}
              id="code-branch"
              defaultValue={branch}
              onChange={(event) => {
                router.push(`${basePath}?branch=${encodeURIComponent(event.target.value)}`);
              }}
            >
              {branches.map((item) => (
                <option disabled={item.archived} key={item.id} value={item.name}>
                  {item.name}
                </option>
              ))}
            </select>
          </div>
          <Link className={styles.toolbarLink} href={repositoryBranchesPath(locale, owner, repository)}>
            <GitBranch aria-hidden="true" size={16} />
            {branches.length} {dictionary.common.branches}
          </Link>
        </div>
        <div className={styles.links}>
          <Link className={styles.toolbarLink} href={commitsPath}>
            <History aria-hidden="true" size={16} />
            {dictionary.codeBrowser.history}
          </Link>
          {parentRevision && (
            <Link
              className={styles.toolbarLink}
              href={`${basePath}/compare?source=${encodeURIComponent(parentRevision)}&target=${encodeURIComponent(
                tree.revision,
              )}`}
            >
              {dictionary.codeBrowser.compare}
            </Link>
          )}
        </div>
      </div>
      <div className={styles.tableWrap}>
        <h2 id="code-browser-title" className="visually-hidden">
          {dictionary.codeBrowser.treeTitle}
        </h2>
        {tree.path && (
          <nav aria-label={dictionary.codeBrowser.breadcrumbLabel} className={styles.pathBar}>
            <ol className={styles.breadcrumb}>
              <li>
                <Link href={`${basePath}?branch=${encodeURIComponent(branch)}`}>{repository}</Link>
              </li>
              {tree.path
                .split("/")
                .filter(Boolean)
                .map((part, index, parts) => (
                  <li key={`${part}-${index}`}>
                    <span aria-hidden="true">/</span>
                    <Link
                      href={`${basePath}?branch=${encodeURIComponent(branch)}&path=${encodeURIComponent(
                        parts.slice(0, index + 1).join("/"),
                      )}`}
                    >
                      {part}
                    </Link>
                  </li>
                ))}
            </ol>
          </nav>
        )}
        <div className={styles.revisionBar}>
          <GitCommitHorizontal aria-hidden="true" size={17} />
          <strong>{dictionary.repository.latestRevision}</strong>
          <code title={tree.revision}>{shortRevision(tree.revision)}</code>
          <Link href={commitsPath}>
            <History aria-hidden="true" size={16} />
            {dictionary.codeBrowser.history}
          </Link>
        </div>
        {tree.entries.length === 0 ? (
          <p className={styles.empty}>{dictionary.codeBrowser.emptyTree}</p>
        ) : (
          <table className={styles.table}>
            <thead>
              <tr>
                <th scope="col">{dictionary.codeBrowser.name}</th>
                <th scope="col">{dictionary.codeBrowser.size}</th>
              </tr>
            </thead>
            <tbody>
              {parentPath !== null && (
                <tr>
                  <td colSpan={2}>
                    <Link
                      href={`${basePath}?branch=${encodeURIComponent(branch)}&path=${encodeURIComponent(parentPath)}`}
                    >
                      {dictionary.codeBrowser.parentDirectory}
                    </Link>
                  </td>
                </tr>
              )}
              {tree.entries.map((entry) => {
                const href =
                  entry.kind === "directory"
                    ? `${basePath}?branch=${encodeURIComponent(branch)}&path=${encodeURIComponent(entry.path)}`
                    : `${basePath}/blob?branch=${encodeURIComponent(branch)}&revision=${encodeURIComponent(
                        tree.revision,
                      )}&path=${encodeURIComponent(entry.path)}`;
                return (
                  <tr key={entry.path}>
                    <td>
                      <span className={styles.entry}>
                        <EntryIcon kind={entry.kind} />
                        <Link href={href}>{entry.name}</Link>
                      </span>
                    </td>
                    <td>{entry.kind === "file" ? formatSize(entry.size, dictionary.codeBrowser.bytes) : "—"}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>
      {tree.hasMore && <p className={styles.revision}>{dictionary.codeBrowser.treeTruncated}</p>}
    </section>
  );
}

function shortRevision(revision: string): string {
  return revision.length > 12 ? revision.slice(0, 12) : revision;
}

function EntryIcon({ kind }: { kind: "directory" | "file" | "link" }) {
  if (kind === "directory") {
    return <Folder aria-hidden="true" size={17} />;
  }
  if (kind === "link") {
    return <Link2 aria-hidden="true" size={17} />;
  }
  return <FileCode2 aria-hidden="true" size={17} />;
}

function parentTreePath(path: string): string | null {
  const parts = path.split("/").filter(Boolean);
  if (parts.length === 0) {
    return null;
  }
  parts.pop();
  return parts.join("/");
}

function formatSize(value: number, unit: string): string {
  return `${value.toLocaleString()} ${unit}`;
}
