"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";

import { AuthRequired } from "@/components/auth/auth-required";
import { PullRequestConversation } from "@/components/repositories/pull-request-conversation";
import { ReviewDiffView } from "@/components/repositories/review-diff-view";
import type {
  AuthSession,
  LoreDiff,
  MergeOperation,
  MergeReadiness,
  MergeRequest,
  MergeRequestComment,
  ReviewSummary,
  ReviewThread,
  RevisionHistoryEntry,
} from "@/lib/api-types";
import { postJson } from "@/lib/auth-client";

import type { Dictionary } from "@/i18n";

import styles from "./pull-request-detail.module.css";

type PullRequestDetailProps = {
  owner: string;
  repository: string;
  locale: "en" | "ja";
  mergeRequest: MergeRequest;
  readiness: MergeReadiness | null;
  reviews: ReviewSummary | null;
  diff: LoreDiff | null;
  commits: RevisionHistoryEntry[];
  comments: MergeRequestComment[];
  commentsAvailable: boolean;
  reviewThreads: ReviewThread[];
  reviewThreadsAvailable: boolean;
  session: AuthSession;
  dictionary: Dictionary;
};

type Tab = "conversation" | "commits" | "files";

export function PullRequestDetail({
  owner,
  repository,
  locale,
  mergeRequest,
  readiness,
  reviews,
  diff,
  commits,
  comments,
  commentsAvailable,
  reviewThreads,
  reviewThreadsAvailable,
  session,
  dictionary,
}: PullRequestDetailProps) {
  const router = useRouter();
  const [tab, setTab] = useState<Tab>("conversation");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [selectedPaths, setSelectedPaths] = useState<string[]>(readiness?.operation?.conflictPaths ?? []);
  const operation = readiness?.operation;
  const csrfToken = sessionCSRF(session);
  const base = mergeRequestPath(owner, repository, mergeRequest.number);

  const mutate = async (path: string, input: unknown, successMessage?: string) => {
    if (!csrfToken) {
      setMessage(dictionary.auth.csrfMissing);
      return;
    }
    setBusy(true);
    setMessage("");
    const result = await postJson<MergeOperation | MergeRequest>(`${base}${path}`, input, csrfToken);
    setBusy(false);
    if (!result.ok) {
      setMessage(
        result.code === "policy_blocked"
          ? dictionary.pullRequestDetail.policyBlocked
          : dictionary.pullRequestDetail.mutationFailed,
      );
      return;
    }
    setMessage(successMessage ?? dictionary.pullRequestDetail.operationState);
    router.refresh();
  };

  const canMutate = isMutableSession(session) && mergeRequest.state === "open" && Boolean(readiness?.canMerge);
  const showStart = canStart(operation, canMutate);
  const showPush = canPush(operation, canMutate, readiness);
  return (
    <div className={styles.layout}>
      <div className={styles.heading}>
        <h1>
          {mergeRequest.title} <span className={styles.meta}>#{mergeRequest.number}</span>
        </h1>
        <p>
          {mergeRequest.author} · <code>{mergeRequest.sourceBranch}</code> → <code>{mergeRequest.targetBranch}</code>
        </p>
      </div>
      <div className={styles.tabs} role="tablist" aria-label={dictionary.common.pullRequests}>
        <TabButton
          active={tab === "conversation"}
          label={dictionary.pullRequestDetail.conversation}
          onClick={() => setTab("conversation")}
        />
        <TabButton
          active={tab === "commits"}
          label={dictionary.pullRequestDetail.commits}
          onClick={() => setTab("commits")}
        />
        <TabButton
          active={tab === "files"}
          label={dictionary.pullRequestDetail.filesChanged}
          onClick={() => setTab("files")}
        />
      </div>
      <div aria-live="polite" className={styles.announcement}>
        {message}
      </div>
      <PullRequestTab
        commits={commits}
        comments={comments}
        commentsAvailable={commentsAvailable}
        diff={diff}
        dictionary={dictionary}
        mergeRequest={mergeRequest}
        reviews={reviews}
        reviewThreads={reviewThreads}
        reviewThreadsAvailable={reviewThreadsAvailable}
        session={session}
        tab={tab}
        locale={locale}
        owner={owner}
        repository={repository}
      />
      {readiness && (
        <MergePanel
          busy={busy}
          canMutate={canMutate}
          dictionary={dictionary}
          operation={operation}
          readiness={readiness}
          selectedPaths={selectedPaths}
          setSelectedPaths={setSelectedPaths}
          showPush={Boolean(showPush)}
          showStart={showStart}
          mutate={mutate}
        />
      )}
      {session.status !== "authenticated" && (
        <AuthRequired
          dictionary={dictionary}
          returnTo={`/${locale}/${owner}/${repository}/pulls/${mergeRequest.number}`}
          session={session}
        />
      )}
    </div>
  );
}

function PullRequestTab({
  tab,
  mergeRequest,
  reviews,
  comments,
  commentsAvailable,
  reviewThreads,
  reviewThreadsAvailable,
  commits,
  diff,
  dictionary,
  session,
  locale,
  owner,
  repository,
}: {
  tab: Tab;
  mergeRequest: MergeRequest;
  reviews: ReviewSummary | null;
  comments: MergeRequestComment[];
  commentsAvailable: boolean;
  reviewThreads: ReviewThread[];
  reviewThreadsAvailable: boolean;
  commits: RevisionHistoryEntry[];
  diff: LoreDiff | null;
  dictionary: Dictionary;
  session: AuthSession;
  locale: "en" | "ja";
  owner: string;
  repository: string;
}) {
  if (tab === "conversation") {
    return (
      <PullRequestConversation
        comments={comments}
        commentsAvailable={commentsAvailable}
        dictionary={dictionary}
        locale={locale}
        mergeRequest={mergeRequest}
        owner={owner}
        repository={repository}
        reviews={reviews}
        session={session}
      />
    );
  }
  if (tab === "commits") {
    return (
      <section aria-labelledby="commits-title" className={styles.panel}>
        <h2 id="commits-title">{dictionary.pullRequestDetail.commits}</h2>
        {commits.length > 0 ? (
          <ul>
            {commits.map((commit) => (
              <li key={commit.revision}>
                <code>{commit.revision}</code> · #{commit.number}
              </li>
            ))}
          </ul>
        ) : (
          <p className={styles.meta}>{dictionary.pullRequestDetail.noChangedFiles}</p>
        )}
      </section>
    );
  }
  return (
    <section aria-labelledby="files-title" className={styles.panel}>
      <h2 id="files-title">{dictionary.pullRequestDetail.filesChanged}</h2>
      {diff ? (
        <ReviewDiffView
          authenticated={session.status === "authenticated"}
          available={reviewThreadsAvailable}
          csrfToken={sessionCSRF(session)}
          dictionary={dictionary}
          diff={diff}
          number={mergeRequest.number}
          owner={owner}
          repository={repository}
          threads={reviewThreads}
        />
      ) : (
        <p className={styles.meta}>{dictionary.pullRequestDetail.noChangedFiles}</p>
      )}
    </section>
  );
}

function sessionCSRF(session: AuthSession): string {
  return session.status === "authenticated" ? session.csrfToken : "";
}

function isMutableSession(session: AuthSession): boolean {
  return session.status === "authenticated";
}

function canStart(operation: MergeOperation | undefined, canMutate: boolean): boolean {
  return (
    canMutate &&
    (!operation ||
      operation.state === "created" ||
      operation.state === "aborted" ||
      (operation.state === "started" && Boolean(operation.errorCode)))
  );
}

function canPush(operation: MergeOperation | undefined, canMutate: boolean, readiness: MergeReadiness | null): boolean {
  if (operation?.state === "pushed" || operation?.state === "pushing") {
    return canMutate;
  }
  return canMutate && operation?.state === "ready_to_push" && Boolean(readiness?.ready);
}

function mergeRequestPath(owner: string, repository: string, number: number): string {
  return `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/merge-requests/${number}`;
}

function TabButton({ active, label, onClick }: { active: boolean; label: string; onClick: () => void }) {
  return (
    <button aria-selected={active} role="tab" type="button" onClick={onClick}>
      {label}
    </button>
  );
}

type MergePanelProps = {
  busy: boolean;
  canMutate: boolean;
  dictionary: Dictionary;
  operation?: MergeOperation;
  readiness: MergeReadiness;
  selectedPaths: string[];
  setSelectedPaths: (paths: string[]) => void;
  showStart: boolean;
  showPush: boolean;
  mutate: (path: string, input: unknown, successMessage?: string) => Promise<void>;
};

function MergePanel({
  busy,
  canMutate,
  dictionary,
  operation,
  readiness,
  selectedPaths,
  setSelectedPaths,
  showStart,
  showPush,
  mutate,
}: MergePanelProps) {
  return (
    <section aria-labelledby="merge-title" className={styles.panel}>
      <h2 id="merge-title">{dictionary.pullRequestDetail.reviewSummary}</h2>
      <MergeStatus dictionary={dictionary} operation={operation} readiness={readiness} />
      {canMutate && operation && operation.conflictPaths.length > 0 && (
        <ConflictResolver
          busy={busy}
          dictionary={dictionary}
          mutate={mutate}
          operation={operation}
          selectedPaths={selectedPaths}
          setSelectedPaths={setSelectedPaths}
        />
      )}
      <MergeActions
        busy={busy}
        canMutate={canMutate}
        dictionary={dictionary}
        mutate={mutate}
        operation={operation}
        readiness={readiness}
        showPush={showPush}
        showStart={showStart}
      />
    </section>
  );
}

function MergeStatus({
  readiness,
  operation,
  dictionary,
}: {
  readiness: MergeReadiness;
  operation?: MergeOperation;
  dictionary: Dictionary;
}) {
  return (
    <>
      <dl className={styles.summary}>
        <div>
          <dt>{dictionary.pullRequestDetail.source}</dt>
          <dd>
            <code>{readiness.currentSourceRevision}</code>
          </dd>
        </div>
        <div>
          <dt>{dictionary.pullRequestDetail.target}</dt>
          <dd>
            <code>{readiness.currentTargetRevision}</code>
          </dd>
        </div>
        <div>
          <dt>{dictionary.pullRequestDetail.ci}</dt>
          <dd>
            {readiness.ciSuccess ? dictionary.pullRequestDetail.ciPassed : dictionary.pullRequestDetail.ciMissing}
          </dd>
        </div>
        <div>
          <dt>{dictionary.pullRequestDetail.operationState}</dt>
          <dd>{operation ? operationStateLabel(operation.state, dictionary) : dictionary.pullRequestDetail.blocked}</dd>
        </div>
      </dl>
      <ReadinessNotice dictionary={dictionary} readiness={readiness} />
      {operation?.errorCode && (
        <div className={styles.status} data-tone="warning">
          {operation.errorCode === "lore_unavailable"
            ? dictionary.pullRequestDetail.operationRecovery
            : dictionary.pullRequestDetail.mutationFailed}
        </div>
      )}
    </>
  );
}

function ReadinessNotice({ readiness, dictionary }: { readiness: MergeReadiness; dictionary: Dictionary }) {
  if (readiness.ready) {
    return (
      <div className={styles.status} data-tone="success">
        {dictionary.pullRequestDetail.ready}
      </div>
    );
  }
  return (
    <div className={styles.blockers}>
      <strong>{dictionary.pullRequestDetail.blocked}</strong>
      <ul>
        {readiness.blockers.map((blocker) => (
          <li key={blocker.code}>{blockerLabel(blocker.code, dictionary)}</li>
        ))}
      </ul>
    </div>
  );
}

function ConflictResolver({
  busy,
  dictionary,
  mutate,
  operation,
  selectedPaths,
  setSelectedPaths,
}: {
  busy: boolean;
  dictionary: Dictionary;
  mutate: (path: string, input: unknown, successMessage?: string) => Promise<void>;
  operation: MergeOperation;
  selectedPaths: string[];
  setSelectedPaths: (paths: string[]) => void;
}) {
  const togglePath = (path: string) => {
    const next = selectedPaths.includes(path)
      ? selectedPaths.filter((value) => value !== path)
      : [...selectedPaths, path];
    setSelectedPaths(next);
  };
  return (
    <div className={styles.conflicts}>
      <h3>{dictionary.pullRequestDetail.conflictTitle}</h3>
      <p>{dictionary.pullRequestDetail.conflictBody}</p>
      <ul>
        {operation.conflictPaths.map((path) => (
          <li key={path}>
            <label>
              <input checked={selectedPaths.includes(path)} type="checkbox" onChange={() => togglePath(path)} />
              <code>{path}</code>
            </label>
          </li>
        ))}
      </ul>
      <div className={styles.actions}>
        <button
          disabled={busy || selectedPaths.length === 0}
          type="button"
          onClick={() =>
            mutate(
              "/merge/conflicts",
              { paths: selectedPaths, strategy: "mine" },
              dictionary.pullRequestDetail.resolveMine,
            )
          }
        >
          {dictionary.pullRequestDetail.resolveMine}
        </button>
        <button
          disabled={busy || selectedPaths.length === 0}
          type="button"
          onClick={() =>
            mutate(
              "/merge/conflicts",
              { paths: selectedPaths, strategy: "theirs" },
              dictionary.pullRequestDetail.resolveTheirs,
            )
          }
        >
          {dictionary.pullRequestDetail.resolveTheirs}
        </button>
      </div>
    </div>
  );
}

type MergeAction = { path: string; label: string; tone?: "secondary" | "danger"; input: unknown };

function MergeActions({
  busy,
  canMutate,
  dictionary,
  mutate,
  operation,
  readiness,
  showStart,
  showPush,
}: {
  busy: boolean;
  canMutate: boolean;
  dictionary: Dictionary;
  mutate: (path: string, input: unknown, successMessage?: string) => Promise<void>;
  operation?: MergeOperation;
  readiness: MergeReadiness;
  showStart: boolean;
  showPush: boolean;
}) {
  if (!canMutate) {
    return (
      <p className={styles.meta}>
        {readiness.canMerge ? dictionary.auth.requiredBody : dictionary.pullRequestDetail.blockers.writePermission}
      </p>
    );
  }
  const actions = mergeActions(dictionary, operation, readiness, showStart, showPush);
  return (
    <div className={styles.actions}>
      {actions.map((action) => (
        <button
          data-tone={action.tone}
          disabled={busy}
          key={action.path}
          type="button"
          onClick={() => mutate(action.path, action.input, action.label)}
        >
          {action.label}
        </button>
      ))}
    </div>
  );
}

function operationStateLabel(state: MergeOperation["state"], dictionary: Dictionary): string {
  const labels = dictionary.pullRequestDetail.states;
  switch (state) {
    case "ready_to_push":
      return labels.readyToPush;
    default:
      return labels[state];
  }
}

function blockerLabel(code: string, dictionary: Dictionary): string {
  const labels = dictionary.pullRequestDetail.blockers;
  switch (code) {
    case "write_permission_required":
      return labels.writePermission;
    case "state_not_open":
      return labels.stateNotOpen;
    case "stale_source_revision":
      return labels.staleSource;
    case "stale_target_revision":
      return labels.staleTarget;
    case "changes_requested":
      return labels.changesRequested;
    case "required_approvals":
      return labels.approvals;
    case "ci_required":
      return labels.ci;
    default:
      return dictionary.pullRequestDetail.policyBlocked;
  }
}

function mergeActions(
  dictionary: Dictionary,
  operation: MergeOperation | undefined,
  readiness: MergeReadiness,
  showStart: boolean,
  showPush: boolean,
): MergeAction[] {
  const actions: MergeAction[] = [];
  if (showStart) {
    const retrying = operation?.state === "started" && Boolean(operation.errorCode);
    actions.push({
      path: retrying ? "/merge/continue" : "/merge/start",
      label: retrying ? dictionary.pullRequestDetail.continueMerge : dictionary.pullRequestDetail.startMerge,
      input: {},
    });
  }
  if (showPush) actions.push({ path: "/merge", label: dictionary.pullRequestDetail.pushMerge, input: {} });
  if (operation && !["aborted", "merged", "pushing", "pushed"].includes(operation.state)) {
    actions.push({ path: "/merge/abort", label: dictionary.pullRequestDetail.abortMerge, input: {}, tone: "danger" });
  }
  if (operation && (readiness.sourceStale || readiness.targetStale)) {
    actions.push({
      path: "/merge/restart",
      label: dictionary.pullRequestDetail.restartMerge,
      input: { paths: [] },
      tone: "secondary",
    });
  }
  return actions;
}
