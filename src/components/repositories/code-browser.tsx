"use client";

import { ChevronDown, Clipboard, Code2, FileCode2, Folder, GitBranch, History, Link2, Search, Tag } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";

import { PopupMenu } from "@/components/ui/popup-menu";
import type { Dictionary } from "@/i18n";
import type { Branch, LoreRevision, LoreTree, RepositoryTag } from "@/lib/api-types";
import { formatRelativeTime, shortRevision } from "@/lib/format";
import { repositoryBranchesPath, repositoryPath, repositoryTagsPath } from "@/lib/routes";

import { UserAvatar } from "../ui/user-avatar";
import { BranchSelector, type BranchSelection } from "./branch-selector";
import styles from "./code-browser.module.css";

type CodeBrowserProps = {
  locale: "en" | "ja";
  owner: string;
  repository: string;
  branch: string;
  branches: Branch[];
  cloneUrl: string;
  currentRevision?: string;
  latestCommit?: LoreRevision;
  tags: RepositoryTag[];
  tree: LoreTree;
  dictionary: Dictionary;
};

export function CodeBrowser({
  locale,
  owner,
  repository,
  branch,
  branches,
  cloneUrl,
  currentRevision,
  latestCommit,
  tags,
  tree,
  dictionary,
}: CodeBrowserProps) {
  const basePath = repositoryPath(locale, owner, repository);
  const router = useRouter();
  const [fileFilter, setFileFilter] = useState("");
  const filteredEntries = useMemo(
    () => filterTreeEntries(tree.entries, fileFilter, locale),
    [fileFilter, locale, tree.entries],
  );

  function selectReference(selection: BranchSelection) {
    router.push(referenceHref(basePath, selection, tree.path));
  }

  return (
    <section aria-labelledby="code-browser-title" className={styles.browser}>
      <CodeToolbar
        branches={branches}
        branch={branch}
        cloneUrl={cloneUrl}
        currentRevision={currentRevision}
        dictionary={dictionary}
        fileFilter={fileFilter}
        locale={locale}
        onFileFilterChange={setFileFilter}
        onSelect={selectReference}
        owner={owner}
        repository={repository}
        tags={tags}
      />
      <div className={styles.tableWrap}>
        <h2 id="code-browser-title" className="visually-hidden">
          {dictionary.codeBrowser.treeTitle}
        </h2>
        <Breadcrumbs
          branch={branch}
          currentRevision={currentRevision}
          dictionary={dictionary}
          locale={locale}
          owner={owner}
          path={tree.path}
          repository={repository}
        />
        <LatestCommitBar
          commitPath={`${basePath}/commit?revision=${encodeURIComponent(tree.revision)}`}
          commitsPath={historyHref(basePath, branch, currentRevision)}
          dictionary={dictionary}
          latestCommit={latestCommit}
          locale={locale}
          revision={tree.revision}
        />
        <FileTable
          branch={branch}
          currentRevision={currentRevision}
          dictionary={dictionary}
          entries={filteredEntries}
          hasEntries={tree.entries.length > 0}
          locale={locale}
          owner={owner}
          repository={repository}
          revision={tree.revision}
        />
      </div>
      {tree.hasMore && <p className={styles.revision}>{dictionary.codeBrowser.treeTruncated}</p>}
    </section>
  );
}

type CodeToolbarProps = {
  branches: Branch[];
  branch: string;
  cloneUrl: string;
  currentRevision?: string;
  dictionary: Dictionary;
  fileFilter: string;
  locale: "en" | "ja";
  onFileFilterChange: (value: string) => void;
  onSelect: (selection: BranchSelection) => void;
  owner: string;
  repository: string;
  tags: RepositoryTag[];
};

function CodeToolbar({
  branches,
  branch,
  cloneUrl,
  currentRevision,
  dictionary,
  fileFilter,
  locale,
  onFileFilterChange,
  onSelect,
  owner,
  repository,
  tags,
}: CodeToolbarProps) {
  const selectedTag = findTag(tags, currentRevision);
  return (
    <div className={styles.toolbar}>
      <div className={styles.branchControls}>
        <BranchSelector
          branches={branches}
          dictionary={dictionary}
          onSelect={onSelect}
          selectedKind={selectedTag ? "tag" : "branch"}
          selectedName={selectedTag?.name ?? branch}
          tags={tags}
        />
        <Link className={styles.toolbarLink} href={repositoryBranchesPath(locale, owner, repository)}>
          <GitBranch aria-hidden="true" size={16} />
          {branches.length} {dictionary.common.branches}
        </Link>
        <Link className={styles.toolbarLink} href={repositoryTagsPath(locale, owner, repository)}>
          <Tag aria-hidden="true" size={16} />
          {tags.length} {dictionary.common.tags}
        </Link>
      </div>
      <div className={styles.toolbarActions}>
        <div className={styles.fileFilter}>
          <Search aria-hidden="true" size={16} />
          <label className="visually-hidden" htmlFor="code-file-filter">
            {dictionary.codeBrowser.goToFile}
          </label>
          <input
            id="code-file-filter"
            onChange={(event) => onFileFilterChange(event.target.value)}
            placeholder={dictionary.codeBrowser.goToFilePlaceholder}
            type="search"
            value={fileFilter}
          />
        </div>
        <CloneMenu cloneUrl={cloneUrl} dictionary={dictionary} />
      </div>
    </div>
  );
}

function CloneMenu({ cloneUrl, dictionary }: { cloneUrl: string; dictionary: Dictionary }) {
  const [copyStatus, setCopyStatus] = useState<"copied" | "failed" | null>(null);

  async function copyCloneURL() {
    if (!cloneUrl) return;
    try {
      await navigator.clipboard.writeText(cloneUrl);
      setCopyStatus("copied");
    } catch {
      setCopyStatus("failed");
    }
  }

  return (
    <PopupMenu
      className={styles.codeMenu}
      panelClassName={styles.codePopover}
      panelRole="none"
      trigger={
        <>
          <Code2 aria-hidden="true" size={16} />
          {dictionary.codeBrowser.code}
          <ChevronDown aria-hidden="true" size={14} />
        </>
      }
      triggerClassName={styles.codeButton}
    >
      {() => (
        <>
          <strong>{dictionary.codeBrowser.cloneRepository}</strong>
          <label htmlFor="repository-clone-url">{dictionary.codeBrowser.cloneURL}</label>
          <div className={styles.cloneURLRow}>
            <input id="repository-clone-url" readOnly value={cloneUrl} />
            <button disabled={!cloneUrl} onClick={() => void copyCloneURL()} type="button">
              <Clipboard aria-hidden="true" size={15} />
              {copyStatus === "copied" ? dictionary.codeBrowser.copied : dictionary.codeBrowser.copy}
            </button>
          </div>
          {copyStatus === "failed" && <p role="status">{dictionary.codeBrowser.copyFailed}</p>}
        </>
      )}
    </PopupMenu>
  );
}

function Breadcrumbs({
  branch,
  currentRevision,
  dictionary,
  locale,
  owner,
  path,
  repository,
}: {
  branch: string;
  currentRevision?: string;
  dictionary: Dictionary;
  locale: "en" | "ja";
  owner: string;
  path: string;
  repository: string;
}) {
  if (!path) return null;
  const basePath = repositoryPath(locale, owner, repository);
  return (
    <nav aria-label={dictionary.codeBrowser.breadcrumbLabel} className={styles.pathBar}>
      <ol className={styles.breadcrumb}>
        <li>
          <Link href={treeHref(basePath, branch, currentRevision)}>{repository}</Link>
        </li>
        {path
          .split("/")
          .filter(Boolean)
          .map((part, index, parts) => (
            <li key={`${part}-${index}`}>
              <span aria-hidden="true">/</span>
              <Link href={treeHref(basePath, branch, currentRevision, parts.slice(0, index + 1).join("/"))}>
                {part}
              </Link>
            </li>
          ))}
      </ol>
    </nav>
  );
}

function LatestCommitBar({
  commitPath,
  commitsPath,
  dictionary,
  latestCommit,
  locale,
  revision,
}: {
  commitPath: string;
  commitsPath: string;
  dictionary: Dictionary;
  latestCommit?: LoreRevision;
  locale: "en" | "ja";
  revision: string;
}) {
  const author = latestCommit?.author?.trim() || dictionary.codeBrowser.unknownAuthor;
  const message = latestCommit?.message?.split("\n")[0].trim() || dictionary.codeBrowser.noCommitMessage;
  return (
    <div aria-label={dictionary.codeBrowser.latestCommit} className={styles.revisionBar} role="group">
      <UserAvatar name={author} size={20} />
      <div className={styles.commitSummary}>
        <strong>{author}</strong>
        <Link href={commitPath} title={message}>
          {message}
        </Link>
      </div>
      <div className={styles.commitMeta}>
        <Link aria-label={`${dictionary.codeBrowser.latestCommit}: ${shortRevision(revision)}`} href={commitPath}>
          <code title={revision}>{shortRevision(revision)}</code>
        </Link>
        <time dateTime={latestCommit?.createdAt}>
          {latestCommit?.createdAt
            ? formatRelativeTime(latestCommit.createdAt, locale)
            : dictionary.codeBrowser.unknownDate}
        </time>
        <Link className={styles.historyLink} href={commitsPath}>
          <History aria-hidden="true" size={16} />
          {dictionary.codeBrowser.history}
        </Link>
      </div>
    </div>
  );
}

function FileTable({
  branch,
  currentRevision,
  dictionary,
  entries,
  hasEntries,
  locale,
  owner,
  repository,
  revision,
}: {
  branch: string;
  currentRevision?: string;
  dictionary: Dictionary;
  entries: LoreTree["entries"];
  hasEntries: boolean;
  locale: "en" | "ja";
  owner: string;
  repository: string;
  revision: string;
}) {
  const basePath = repositoryPath(locale, owner, repository);
  if (!hasEntries) return <p className={styles.empty}>{dictionary.codeBrowser.emptyTree}</p>;
  if (entries.length === 0) return <p className={styles.empty}>{dictionary.codeBrowser.noMatchingFiles}</p>;
  return (
    <table className={styles.table}>
      <thead>
        <tr>
          <th scope="col">{dictionary.codeBrowser.name}</th>
          <th scope="col">{dictionary.codeBrowser.size}</th>
        </tr>
      </thead>
      <tbody>
        {entries.map((entry) => {
          const href =
            entry.kind === "directory"
              ? treeHref(basePath, branch, currentRevision, entry.path)
              : fileHref(basePath, branch, currentRevision, revision, entry.path);
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
  );
}

function findTag(tags: RepositoryTag[], revision: string | undefined): RepositoryTag | undefined {
  return revision ? tags.find((tag) => tag.revision === revision) : undefined;
}

function filterTreeEntries(entries: LoreTree["entries"], filter: string, locale: "en" | "ja") {
  const normalizedFilter = filter.trim().toLocaleLowerCase();
  return entries
    .filter((entry) => matchesEntry(entry, normalizedFilter))
    .sort((left, right) => compareEntries(left, right, locale));
}

function matchesEntry(entry: LoreTree["entries"][number], filter: string): boolean {
  return (
    filter === "" || entry.name.toLocaleLowerCase().includes(filter) || entry.path.toLocaleLowerCase().includes(filter)
  );
}

function compareEntries(
  left: LoreTree["entries"][number],
  right: LoreTree["entries"][number],
  locale: "en" | "ja",
): number {
  const leftIsDirectory = left.kind === "directory";
  const rightIsDirectory = right.kind === "directory";
  if (leftIsDirectory !== rightIsDirectory) return leftIsDirectory ? -1 : 1;
  return left.name.localeCompare(right.name, locale);
}

function referenceHref(basePath: string, selection: BranchSelection, path: string): string {
  const query = new URLSearchParams(
    selection.kind === "branch" ? { branch: selection.name } : { revision: selection.revision },
  );
  if (path) query.set("path", path);
  return `${basePath}?${query.toString()}`;
}

function treeHref(basePath: string, branch: string, currentRevision: string | undefined, path?: string): string {
  const query = new URLSearchParams(currentRevision ? { revision: currentRevision } : { branch });
  if (path) query.set("path", path);
  return `${basePath}?${query.toString()}`;
}

function historyHref(basePath: string, branch: string, currentRevision: string | undefined): string {
  const query = new URLSearchParams(currentRevision ? { revision: currentRevision } : { branch });
  return `${basePath}/commits?${query.toString()}`;
}

function fileHref(
  basePath: string,
  branch: string,
  currentRevision: string | undefined,
  revision: string,
  path: string,
) {
  const query = new URLSearchParams({ revision, path });
  if (!currentRevision) query.set("branch", branch);
  return `${basePath}/blob?${query.toString()}`;
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

function formatSize(value: number, unit: string): string {
  return `${value.toLocaleString()} ${unit}`;
}
