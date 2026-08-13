"use client";

import { ChevronDown, FileCode2, Folder } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { LoreDiff, LoreDiffFile, PendingReview, ReviewThread } from "@/lib/api-types";
import { abbreviateCount } from "@/lib/format";
import { startPendingReview } from "@/lib/pending-review-client";
import { parseReviewDiff, type ReviewDiffRow } from "@/lib/review-diff";
import { createReviewThread } from "@/lib/review-thread-client";

import { CopyButton } from "../ui/copy-button";
import { PendingReviewBar } from "./pending-review-bar";
import styles from "./review-diff-view.module.css";
import { ReviewThreadCard } from "./review-thread-card";

type Anchor = { path: string; side: "left" | "right"; lineNumber: number };

type ReviewDiffViewProps = {
  diff: LoreDiff;
  threads: ReviewThread[];
  available: boolean;
  owner: string;
  repository: string;
  number: number;
  locale: Locale;
  csrfToken: string;
  authenticated: boolean;
  dictionary: Dictionary;
  pendingReview: PendingReview | null;
};

type ComposerMode = "single" | "review";

type DiffFileStatus = "added" | "removed" | "modified";

type DiffFile = {
  file: LoreDiffFile;
  rows: ReviewDiffRow[];
  additions: number;
  deletions: number;
  anchorId: string;
  status: DiffFileStatus;
};

type FileTreeDirectory = {
  kind: "directory";
  name: string;
  children: FileTreeNode[];
};

type FileTreeFile = {
  kind: "file";
  name: string;
  entry: DiffFile;
};

type FileTreeNode = FileTreeDirectory | FileTreeFile;

export function ReviewDiffView(props: ReviewDiffViewProps) {
  const router = useRouter();
  const [threads, setThreads] = useState(props.threads);
  const [pending, setPending] = useState(props.pendingReview);
  const [anchor, setAnchor] = useState<Anchor | null>(null);
  const [body, setBody] = useState("");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState(
    props.available ? "" : props.dictionary.pullRequestDetail.reviewThreadsUnavailable,
  );

  // A batched comment needs a pending review to hang on, so the first one
  // starts the review before the comment is posted.
  const resolvePendingReview = async (mode: ComposerMode) => {
    if (mode === "single") return null;
    if (pending) return pending;
    const started = await startPendingReview(props.owner, props.repository, props.number, props.csrfToken);
    if (!started.ok) return null;
    setPending(started.data);
    return started.data;
  };

  const submitThread = async (mode: ComposerMode) => {
    if (!anchor || !body.trim() || !props.csrfToken) return;
    setBusy(true);
    setMessage("");
    const review = await resolvePendingReview(mode);
    if (mode === "review" && !review) {
      setBusy(false);
      setMessage(props.dictionary.pendingReviews.startFailed);
      return;
    }
    const result = await createReviewThread(
      props.owner,
      props.repository,
      props.number,
      {
        ...anchor,
        body,
        expectedBaseRevision: props.diff.source,
        expectedHeadRevision: props.diff.target,
        pendingReviewId: review?.id,
      },
      props.csrfToken,
    );
    setBusy(false);
    if (!result.ok) {
      setMessage(mutationMessage(result.kind, props.dictionary));
      return;
    }
    setThreads((current) => [...current, result.data]);
    if (review) countPendingComment();
    setAnchor(null);
    setBody("");
  };

  const countPendingComment = () => {
    setPending((current) => (current ? { ...current, commentCount: current.commentCount + 1 } : current));
  };

  // Submitting publishes the batched comments and abandoning removes them, so
  // the shown threads follow before the server data is reloaded.
  const finishPendingReview = (submitted: boolean) => {
    setPending(null);
    setThreads((current) =>
      current
        .map((thread) => ({
          ...thread,
          comments: submitted
            ? thread.comments.map((comment) => ({ ...comment, pending: false }))
            : thread.comments.filter((comment) => !comment.pending),
        }))
        .filter((thread) => thread.comments.length > 0),
    );
    setMessage(submitted ? props.dictionary.pendingReviews.submitted : "");
    router.refresh();
  };

  const updateThread = (thread: ReviewThread) => {
    setThreads((current) => current.map((item) => (item.id === thread.id ? thread : item)));
  };

  const outdated = threads.filter((thread) => thread.outdated);
  const files = useMemo(
    () => props.diff.files.map((file, index) => describeFile(file, `review-diff-file-${index}`)),
    [props.diff.files],
  );
  if (props.diff.files.length === 0) {
    return <p className={styles.meta}>{props.dictionary.pullRequestDetail.noChangedFiles}</p>;
  }
  const stats = files.reduce(
    (total, file) => ({
      additions: total.additions + file.additions,
      deletions: total.deletions + file.deletions,
    }),
    { additions: 0, deletions: 0 },
  );
  return (
    <div className={styles.files}>
      <DiffStatHeader dictionary={props.dictionary} files={files.length} locale={props.locale} stats={stats} />
      {pending && (
        <PendingReviewBar
          csrfToken={props.csrfToken}
          dictionary={props.dictionary}
          locale={props.locale}
          number={props.number}
          onDiscarded={() => finishPendingReview(false)}
          onSubmitted={() => finishPendingReview(true)}
          owner={props.owner}
          pendingReview={pending}
          repository={props.repository}
        />
      )}
      <div aria-live="polite" className={styles.message}>
        {message}
      </div>
      <div className={styles.reviewLayout}>
        <ReviewDiffFileTree dictionary={props.dictionary} entries={files} />
        <div className={styles.fileList}>
          {files.map((entry) => (
            <ReviewDiffFileCard
              anchor={anchor}
              authenticated={props.authenticated}
              busy={busy}
              dictionary={props.dictionary}
              entry={entry}
              key={entry.file.path}
              onCancel={() => setAnchor(null)}
              onChangeBody={setBody}
              onPendingComment={countPendingComment}
              onSelect={setAnchor}
              onSubmit={submitThread}
              pendingReviewId={pending?.id ?? null}
              props={props}
              threadBody={body}
              threads={threads}
              updateThread={updateThread}
            />
          ))}
          {outdated.length > 0 && (
            <section className={styles.outdated}>
              <h3>{props.dictionary.pullRequestDetail.outdatedConversations}</h3>
              {outdated.map((thread) => (
                <ReviewThreadCard
                  key={thread.id}
                  onPendingComment={countPendingComment}
                  pendingReviewId={pending?.id ?? null}
                  thread={thread}
                  updateThread={updateThread}
                  {...props}
                />
              ))}
            </section>
          )}
          {(props.diff.hasMore || props.diff.truncated) && (
            <p className={styles.meta}>{props.dictionary.codeBrowser.diffTruncated}</p>
          )}
        </div>
      </div>
    </div>
  );
}

function DiffStatHeader({
  dictionary,
  files,
  locale,
  stats,
}: {
  dictionary: Dictionary;
  files: number;
  locale: Locale;
  stats: { additions: number; deletions: number };
}) {
  const fileCount = abbreviateCount(files, locale);
  const additions = abbreviateCount(stats.additions, locale);
  const deletions = abbreviateCount(stats.deletions, locale);
  const squares = diffBarSquares(stats);
  const barLabel = `${fileCount} ${dictionary.pullRequestDetail.changedFiles}, +${additions} ${
    dictionary.pullRequestDetail.additions
  }, -${deletions} ${dictionary.pullRequestDetail.deletions}`;
  return (
    <div className={styles.statHeader}>
      <div className={styles.statSummary}>
        <strong>{dictionary.pullRequestDetail.changedFilesCount.replace("{count}", fileCount)}</strong>
        <span className={styles.statCount}>
          <span className={styles.additions}>+{additions}</span> {dictionary.pullRequestDetail.additions}
        </span>
        <span className={styles.statCount}>
          <span className={styles.deletions}>-{deletions}</span> {dictionary.pullRequestDetail.deletions}
        </span>
      </div>
      <div aria-label={barLabel} className={styles.diffBar}>
        {squares.map((kind, index) => (
          <span aria-hidden="true" className={styles.diffBarSquare} data-kind={kind} key={`${kind}-${index}`} />
        ))}
      </div>
    </div>
  );
}

function ReviewDiffFileTree({ dictionary, entries }: { dictionary: Dictionary; entries: DiffFile[] }) {
  const tree = useMemo(() => buildFileTree(entries), [entries]);
  return (
    <aside aria-label={dictionary.pullRequestDetail.changedFiles} className={styles.fileTree}>
      <details className={styles.treeDetails} open>
        <summary className={styles.treeSummary}>
          <ChevronDown aria-hidden="true" className={styles.treeChevron} size={16} />
          <strong>{dictionary.pullRequestDetail.changedFiles}</strong>
          <span className={styles.treeCount}>{entries.length}</span>
        </summary>
        <ul className={styles.treeList}>
          {tree.map((node) => (
            <FileTreeNodeView dictionary={dictionary} key={`${node.kind}-${node.name}`} node={node} />
          ))}
        </ul>
      </details>
    </aside>
  );
}

function FileTreeNodeView({ dictionary, node }: { dictionary: Dictionary; node: FileTreeNode }) {
  if (node.kind === "directory") {
    return (
      <li>
        <details className={styles.treeFolder} open>
          <summary className={styles.treeFolderSummary}>
            <ChevronDown aria-hidden="true" className={styles.treeChevron} size={14} />
            <Folder aria-hidden="true" size={14} />
            <span>{node.name}</span>
          </summary>
          <ul className={styles.treeListNested}>
            {node.children.map((child) => (
              <FileTreeNodeView dictionary={dictionary} key={`${child.kind}-${child.name}`} node={child} />
            ))}
          </ul>
        </details>
      </li>
    );
  }
  return (
    <li>
      <a
        aria-label={`${node.entry.file.path} (${actionLabel(node.entry.file.action, dictionary)})`}
        className={styles.treeFile}
        href={`#${node.entry.anchorId}`}
        title={node.entry.file.path}
      >
        <span aria-hidden="true" className={styles.statusDot} data-status={node.entry.status} />
        <FileCode2 aria-hidden="true" size={14} />
        <span className={styles.treeFileName}>{node.name}</span>
      </a>
    </li>
  );
}

function ReviewDiffFileCard({
  anchor,
  authenticated,
  busy,
  dictionary,
  entry,
  onCancel,
  onChangeBody,
  onPendingComment,
  onSelect,
  onSubmit,
  pendingReviewId,
  props,
  threadBody,
  threads,
  updateThread,
}: {
  anchor: Anchor | null;
  authenticated: boolean;
  busy: boolean;
  dictionary: Dictionary;
  entry: DiffFile;
  onCancel: () => void;
  onChangeBody: (body: string) => void;
  onPendingComment: () => void;
  onSelect: (anchor: Anchor) => void;
  onSubmit: (mode: ComposerMode) => Promise<void>;
  pendingReviewId: string | null;
  props: ReviewDiffViewProps;
  threadBody: string;
  threads: ReviewThread[];
  updateThread: (thread: ReviewThread) => void;
}) {
  const [viewed, setViewed] = useState(false);
  const [open, setOpen] = useState(true);
  const storageKey = viewedStorageKey(props, entry.file.path);
  const currentThreads = threads.filter((thread) => thread.path === entry.file.path && !thread.outdated);

  useEffect(() => {
    let stored = false;
    try {
      stored = window.localStorage.getItem(storageKey) === "true";
    } catch {
      stored = false;
    }
    // Synchronize the client-only preference after hydration.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setViewed(stored);
    setOpen(!stored);
  }, [storageKey]);

  function toggleViewed(nextViewed: boolean) {
    setViewed(nextViewed);
    setOpen(!nextViewed);
    try {
      if (nextViewed) {
        window.localStorage.setItem(storageKey, "true");
      } else {
        window.localStorage.removeItem(storageKey);
      }
    } catch {
      // The checkbox remains usable when storage is unavailable.
    }
  }

  return (
    <details
      className={styles.file}
      id={entry.anchorId}
      onToggle={(event) => setOpen(event.currentTarget.open)}
      open={open}
    >
      <summary className={styles.fileHeader}>
        <ChevronDown aria-hidden="true" className={styles.chevron} size={16} />
        <span className={styles.filePathGroup}>
          <span className={styles.filePath} title={entry.file.path}>
            {entry.file.path}
          </span>
          <span onClick={(event) => event.stopPropagation()}>
            <CopyButton
              copiedLabel={dictionary.pullRequestDetail.filePathCopied}
              label={dictionary.pullRequestDetail.copyFilePath}
              value={entry.file.path}
            />
          </span>
        </span>
        <span className={styles.fileCounts}>
          <span className={styles.additions}>+{entry.additions}</span>
          <span className={styles.deletions}>-{entry.deletions}</span>
        </span>
        <label className={styles.viewed} onClick={(event) => event.stopPropagation()}>
          <input checked={viewed} onChange={(event) => toggleViewed(event.target.checked)} type="checkbox" />
          {dictionary.pullRequestDetail.viewed}
        </label>
      </summary>
      {entry.file.binary || !entry.file.patch ? (
        <p className={styles.meta}>
          {entry.file.binary ? dictionary.codeBrowser.binary : dictionary.codeBrowser.diffTruncated}
        </p>
      ) : (
        <table className={styles.diffTable}>
          <tbody>
            {entry.rows.map((row) => (
              <DiffRow
                {...props}
                anchor={anchor}
                authenticated={authenticated}
                busy={busy}
                dictionary={dictionary}
                key={row.key}
                onCancel={onCancel}
                onChangeBody={onChangeBody}
                onPendingComment={onPendingComment}
                onSelect={onSelect}
                onSubmit={onSubmit}
                path={entry.file.path}
                pendingReviewId={pendingReviewId}
                row={row}
                threadBody={threadBody}
                threads={threadsForRow(currentThreads, row)}
                updateThread={updateThread}
              />
            ))}
          </tbody>
        </table>
      )}
      {entry.file.truncated && <p className={styles.meta}>{dictionary.codeBrowser.diffTruncated}</p>}
    </details>
  );
}

type DiffRowProps = ReviewDiffViewProps & {
  path: string;
  row: ReviewDiffRow;
  threads: ReviewThread[];
  anchor: Anchor | null;
  threadBody: string;
  busy: boolean;
  onSelect: (anchor: Anchor) => void;
  onCancel: () => void;
  onChangeBody: (body: string) => void;
  onSubmit: (mode: ComposerMode) => Promise<void>;
  onPendingComment: () => void;
  pendingReviewId: string | null;
  updateThread: (thread: ReviewThread) => void;
};

function DiffRow(props: DiffRowProps) {
  const { row } = props;
  if (row.kind === "header") {
    return (
      <tr className={styles.hunk}>
        <td colSpan={3}>{row.content}</td>
      </tr>
    );
  }
  const selected =
    props.anchor?.path === props.path &&
    ((props.anchor.side === "left" && props.anchor.lineNumber === row.oldLine) ||
      (props.anchor.side === "right" && props.anchor.lineNumber === row.newLine));
  return (
    <>
      <tr className={styles.line} data-kind={row.kind}>
        <LineNumber
          authenticated={props.authenticated}
          dictionary={props.dictionary}
          line={row.oldLine}
          onSelect={(lineNumber) => props.onSelect({ path: props.path, side: "left", lineNumber })}
          side="left"
        />
        <LineNumber
          authenticated={props.authenticated}
          dictionary={props.dictionary}
          line={row.newLine}
          onSelect={(lineNumber) => props.onSelect({ path: props.path, side: "right", lineNumber })}
          side="right"
        />
        <td className={styles.code}>
          <code>{row.content || " "}</code>
        </td>
      </tr>
      {selected && (
        <tr>
          <td className={styles.inline} colSpan={3}>
            <ReviewComposer
              body={props.threadBody}
              busy={props.busy}
              dictionary={props.dictionary}
              onCancel={props.onCancel}
              onChange={props.onChangeBody}
              onSubmit={props.onSubmit}
              pendingReviewId={props.pendingReviewId}
            />
          </td>
        </tr>
      )}
      {props.threads.map((thread) => (
        <tr key={thread.id}>
          <td className={styles.inline} colSpan={3}>
            <ReviewThreadCard
              {...props}
              onPendingComment={props.onPendingComment}
              pendingReviewId={props.pendingReviewId}
              thread={thread}
              updateThread={props.updateThread}
            />
          </td>
        </tr>
      ))}
    </>
  );
}

function LineNumber({
  line,
  side,
  authenticated,
  dictionary,
  onSelect,
}: {
  line: number | null;
  side: "left" | "right";
  authenticated: boolean;
  dictionary: Dictionary;
  onSelect: (line: number) => void;
}) {
  if (line === null) return <td className={styles.lineNumber} />;
  const label = dictionary.pullRequestDetail.commentOnLine.replace("{line}", String(line));
  return (
    <td className={styles.lineNumber}>
      {authenticated ? (
        <button aria-label={`${label} (${side})`} onClick={() => onSelect(line)} type="button">
          {line}
        </button>
      ) : (
        line
      )}
    </td>
  );
}

function ReviewComposer({
  body,
  busy,
  dictionary,
  onChange,
  onCancel,
  onSubmit,
  pendingReviewId,
}: {
  body: string;
  busy: boolean;
  dictionary: Dictionary;
  onChange: (value: string) => void;
  onCancel: () => void;
  onSubmit: (mode: ComposerMode) => Promise<void>;
  pendingReviewId: string | null;
}) {
  return (
    <div className={styles.composer}>
      <textarea
        aria-label={dictionary.pullRequestDetail.newThreadPlaceholder}
        onChange={(event) => onChange(event.target.value)}
        placeholder={dictionary.pullRequestDetail.newThreadPlaceholder}
        rows={4}
        value={body}
      />
      <div className={styles.actions}>
        <button disabled={busy} onClick={onCancel} type="button">
          {dictionary.common.cancel}
        </button>
        <button disabled={busy || !body.trim()} onClick={() => void onSubmit("single")} type="button">
          {dictionary.pendingReviews.addSingleComment}
        </button>
        <button disabled={busy || !body.trim()} onClick={() => void onSubmit("review")} type="button">
          {pendingReviewId ? dictionary.pendingReviews.addReviewComment : dictionary.pendingReviews.startReview}
        </button>
      </div>
    </div>
  );
}

function threadsForRow(threads: ReviewThread[], row: ReviewDiffRow): ReviewThread[] {
  return threads.filter(
    (thread) =>
      (thread.side === "left" && thread.lineNumber === row.oldLine) ||
      (thread.side === "right" && thread.lineNumber === row.newLine),
  );
}

function mutationMessage(kind: string, dictionary: Dictionary): string {
  return kind === "conflict"
    ? dictionary.pullRequestDetail.reviewConflict
    : dictionary.pullRequestDetail.reviewMutationFailed;
}

function actionLabel(action: string, dictionary: Dictionary): string {
  switch (action) {
    case "added":
      return dictionary.codeBrowser.actions.added;
    case "deleted":
    case "removed":
      return dictionary.codeBrowser.actions.deleted;
    case "moved":
      return dictionary.codeBrowser.actions.moved;
    case "copied":
      return dictionary.codeBrowser.actions.copied;
    default:
      return dictionary.codeBrowser.actions.modified;
  }
}

function describeFile(file: LoreDiffFile, anchorId: string): DiffFile {
  const rows = file.binary ? [] : parseReviewDiff(file);
  return {
    file,
    rows,
    additions: rows.filter((row) => row.kind === "added").length,
    deletions: rows.filter((row) => row.kind === "deleted").length,
    anchorId,
    status: fileStatus(file.action),
  };
}

function fileStatus(action: string): DiffFileStatus {
  if (action === "added") return "added";
  if (action === "deleted" || action === "removed") return "removed";
  return "modified";
}

function buildFileTree(entries: DiffFile[]): FileTreeNode[] {
  const root: FileTreeDirectory = { kind: "directory", name: "", children: [] };
  for (const entry of entries) {
    const parts = entry.file.path.split("/").filter(Boolean);
    const fileName = parts.pop() ?? entry.file.path;
    let directory = root;
    for (const part of parts) {
      let child = directory.children.find(
        (node): node is FileTreeDirectory => node.kind === "directory" && node.name === part,
      );
      if (!child) {
        child = { kind: "directory", name: part, children: [] };
        directory.children.push(child);
      }
      directory = child;
    }
    directory.children.push({ kind: "file", name: fileName, entry });
  }
  return sortFileTree(root.children);
}

function sortFileTree(nodes: FileTreeNode[]): FileTreeNode[] {
  return [...nodes]
    .sort((left, right) => {
      if (left.kind !== right.kind) return left.kind === "directory" ? -1 : 1;
      return left.name.localeCompare(right.name);
    })
    .map((node) => (node.kind === "directory" ? { ...node, children: sortFileTree(node.children) } : node));
}

type DiffBarKind = "additions" | "deletions" | "empty";

function diffBarSquares(stats: { additions: number; deletions: number }): DiffBarKind[] {
  const total = stats.additions + stats.deletions;
  if (total === 0) return ["empty"];
  const squareCount = Math.min(50, total);
  let additions = stats.additions > 0 ? Math.max(1, Math.round((squareCount * stats.additions) / total)) : 0;
  let deletions = stats.deletions > 0 ? Math.max(1, squareCount - additions) : 0;
  while (additions + deletions > squareCount) {
    if (additions >= deletions && additions > 1) additions -= 1;
    else if (deletions > 1) deletions -= 1;
    else break;
  }
  return [
    ...Array.from({ length: additions }, () => "additions" as const),
    ...Array.from({ length: deletions }, () => "deletions" as const),
  ];
}

function viewedStorageKey(props: ReviewDiffViewProps, path: string): string {
  return [
    "lorehub",
    "pull-request-diff-viewed",
    props.owner,
    props.repository,
    props.number,
    props.diff.source,
    props.diff.target,
    path,
  ]
    .map((part) => encodeURIComponent(String(part)))
    .join(":");
}
