"use client";

import {
  Check,
  CircleAlert,
  CircleCheck,
  CircleDot,
  Copy,
  FilePenLine,
  GitMerge,
  GitPullRequestClosed,
} from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";

import { AuthRequired } from "@/components/auth/auth-required";
import { CommitStatusList } from "@/components/repositories/commit-status-list";
import { PullRequestConversation } from "@/components/repositories/pull-request-conversation";
import { PullRequestMetadata } from "@/components/repositories/pull-request-metadata";
import { ReviewDiffView } from "@/components/repositories/review-diff-view";
import { UserAvatar } from "@/components/ui/user-avatar";
import type {
  Assignee,
  AuthSession,
  Label,
  LoreDiff,
  MergeOperation,
  MergeReadiness,
  MergeRequest,
  MergeRequestComment,
  MergeRequestMetadata,
  Milestone,
  ReviewCandidate,
  ReviewRequestSummary,
  ReviewSummary,
  ReviewThread,
  RevisionHistoryEntry,
} from "@/lib/api-types";
import { postJson } from "@/lib/auth-client";
import type { CommentPage } from "@/lib/comment-page-types";
import { abbreviateCount, shortRevision } from "@/lib/format";
import { repositoryPath } from "@/lib/routes";
import type { WorkItemEvent } from "@/lib/work-item-events";

import type { Dictionary } from "@/i18n";

import styles from "./pull-request-detail.module.css";

type PullRequestDetailProps = {
  owner: string;
  repository: string;
  locale: "en" | "ja";
  mergeRequest: MergeRequest;
  readiness: MergeReadiness | null;
  readinessUnavailableReason: "forbidden" | "unavailable";
  reviews: ReviewSummary | null;
  reviewCandidates: ReviewCandidate[];
  reviewRequests: ReviewRequestSummary | null;
  diff: LoreDiff | null;
  commits: RevisionHistoryEntry[];
  comments: CommentPage<MergeRequestComment> | null;
  events: WorkItemEvent[];
  assignees: Assignee[];
  assigneesAvailable: boolean;
  labels: Label[];
  labelsAvailable: boolean;
  metadata: MergeRequestMetadata | null;
  milestones: Milestone[];
  milestonesAvailable: boolean;
  reviewThreads: ReviewThread[];
  reviewThreadsAvailable: boolean;
  session: AuthSession;
  dictionary: Dictionary;
  initialTab: PullRequestTab;
};

export type PullRequestTab = "conversation" | "commits" | "checks" | "files";

export function PullRequestDetail({
  owner,
  repository,
  locale,
  mergeRequest,
  readiness,
  readinessUnavailableReason,
  reviews,
  reviewCandidates,
  reviewRequests,
  diff,
  commits,
  comments,
  events,
  assignees,
  assigneesAvailable,
  labels,
  labelsAvailable,
  metadata,
  milestones,
  milestonesAvailable,
  reviewThreads,
  reviewThreadsAvailable,
  session,
  dictionary,
  initialTab,
}: PullRequestDetailProps) {
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [selectedPaths, setSelectedPaths] = useState<string[]>(readiness?.operation?.conflictPaths ?? []);
  const operation = readiness?.operation;
  const csrfToken = sessionCSRF(session);
  const base = mergeRequestPath(owner, repository, mergeRequest.number);
  const pullRequestPath = `${repositoryPath(locale, owner, repository, "pulls")}/${mergeRequest.number}`;

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

  const canMutate = canMergeRequest(session, mergeRequest, readiness);
  const showStart = canStart(operation, canMutate);
  const showPush = canPush(operation, canMutate, readiness);
  const tab = initialTab;
  const counts = pullRequestTabCounts(comments, commits, readiness, diff);
  return (
    <div className={styles.layout}>
      <header className={styles.heading}>
        <div className={styles.titleRow}>
          <h1>
            {mergeRequest.title} <span className={styles.meta}>#{mergeRequest.number}</span>
          </h1>
          <StateBadge dictionary={dictionary} mergeRequest={mergeRequest} />
        </div>
        <p className={styles.branchSummary}>
          <UserAvatar name={mergeRequest.author} size={24} />
          <span>
            {dictionary.pullRequestDetail.mergeSummary
              .replace("{author}", mergeRequest.author)
              .replace("{count}", abbreviateCount(commits.length, locale))}
          </span>
          <BranchChip>{mergeRequest.targetBranch}</BranchChip>
          <span>{dictionary.pullRequestDetail.from}</span>
          <BranchChip>{mergeRequest.sourceBranch}</BranchChip>
        </p>
      </header>
      <nav aria-label={dictionary.common.pullRequests} className={styles.tabs} role="tablist">
        <TabLink
          active={tab === "conversation"}
          count={counts.conversation}
          href={tabHref(pullRequestPath, "conversation")}
          id="pull-request-tab-conversation"
          label={dictionary.pullRequestDetail.conversation}
          panelId="pull-request-panel-conversation"
          locale={locale}
        />
        <TabLink
          active={tab === "commits"}
          count={counts.commits}
          href={tabHref(pullRequestPath, "commits")}
          id="pull-request-tab-commits"
          label={dictionary.pullRequestDetail.commits}
          panelId="pull-request-panel-commits"
          locale={locale}
        />
        <TabLink
          active={tab === "checks"}
          count={counts.checks}
          href={tabHref(pullRequestPath, "checks")}
          id="pull-request-tab-checks"
          label={dictionary.pullRequestDetail.checks}
          panelId="pull-request-panel-checks"
          locale={locale}
        />
        <TabLink
          active={tab === "files"}
          count={counts.files}
          href={tabHref(pullRequestPath, "files")}
          id="pull-request-tab-files"
          label={dictionary.pullRequestDetail.filesChanged}
          panelId="pull-request-panel-files"
          locale={locale}
        />
      </nav>
      <div aria-live="polite" className={styles.announcement}>
        {message}
      </div>
      <div className={styles.columns}>
        <div className={styles.main}>
          <PullRequestTab
            commits={commits}
            comments={comments}
            diff={diff}
            events={events}
            dictionary={dictionary}
            mergeRequest={mergeRequest}
            readiness={readiness}
            readinessUnavailableReason={readinessUnavailableReason}
            reviews={reviews}
            reviewCandidates={reviewCandidates}
            reviewRequests={reviewRequests}
            reviewThreads={reviewThreads}
            reviewThreadsAvailable={reviewThreadsAvailable}
            session={session}
            tab={tab}
            locale={locale}
            owner={owner}
            repository={repository}
          />
          {tab === "conversation" && readiness && (
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
              authenticated={session.status === "authenticated"}
            />
          )}
        </div>
        <div className={styles.sidebar}>
          <PullRequestMetadata
            assignees={assignees}
            assigneesAvailable={assigneesAvailable}
            csrfToken={csrfToken}
            dictionary={dictionary}
            labels={labels}
            labelsAvailable={labelsAvailable}
            metadata={metadata}
            milestones={milestones}
            milestonesAvailable={milestonesAvailable}
            number={mergeRequest.number}
            owner={owner}
            repository={repository}
          />
        </div>
      </div>
      {session.status !== "authenticated" && (
        <AuthRequired dictionary={dictionary} returnTo={pullRequestPath} session={session} />
      )}
    </div>
  );
}

function PullRequestTab({
  tab,
  mergeRequest,
  reviews,
  reviewCandidates,
  reviewRequests,
  comments,
  events,
  reviewThreads,
  reviewThreadsAvailable,
  commits,
  diff,
  dictionary,
  session,
  locale,
  owner,
  repository,
  readiness,
  readinessUnavailableReason,
}: {
  tab: PullRequestTab;
  mergeRequest: MergeRequest;
  reviews: ReviewSummary | null;
  reviewCandidates: ReviewCandidate[];
  reviewRequests: ReviewRequestSummary | null;
  comments: CommentPage<MergeRequestComment> | null;
  events: WorkItemEvent[];
  readiness: MergeReadiness | null;
  readinessUnavailableReason: "forbidden" | "unavailable";
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
      <div
        aria-labelledby="pull-request-tab-conversation"
        className={styles.tabpanel}
        id="pull-request-panel-conversation"
        role="tabpanel"
        tabIndex={0}
      >
        <PullRequestConversation
          comments={comments}
          dictionary={dictionary}
          events={events}
          locale={locale}
          mergeRequest={mergeRequest}
          owner={owner}
          repository={repository}
          reviews={reviews}
          reviewCandidates={reviewCandidates}
          reviewRequests={reviewRequests}
          session={session}
        />
      </div>
    );
  }
  if (tab === "checks") {
    return (
      <section
        aria-labelledby="pull-request-tab-checks"
        className={styles.panel}
        id="pull-request-panel-checks"
        role="tabpanel"
        tabIndex={0}
      >
        <h2 id="pull-request-panel-checks-title">{dictionary.pullRequestDetail.checks}</h2>
        <CommitStatusList
          dictionary={dictionary}
          locale={locale}
          statuses={readiness ? readiness.statusChecks : null}
          unavailableReason={readiness ? undefined : readinessUnavailableReason}
        />
      </section>
    );
  }
  if (tab === "commits") {
    return (
      <section
        aria-labelledby="pull-request-tab-commits"
        className={styles.panel}
        id="pull-request-panel-commits"
        role="tabpanel"
        tabIndex={0}
      >
        <h2 id="pull-request-panel-commits-title">{dictionary.pullRequestDetail.commits}</h2>
        {commits.length > 0 ? (
          <ul className={styles.commitList}>
            {commits.map((commit) => (
              <CommitRow
                commit={commit}
                dictionary={dictionary}
                key={commit.revision}
                locale={locale}
                owner={owner}
                repository={repository}
              />
            ))}
          </ul>
        ) : (
          <p className={styles.meta}>{dictionary.pullRequestDetail.noCommits}</p>
        )}
      </section>
    );
  }
  return (
    <section
      aria-labelledby="pull-request-tab-files"
      className={styles.panel}
      id="pull-request-panel-files"
      role="tabpanel"
      tabIndex={0}
    >
      <h2 id="pull-request-panel-files-title">{dictionary.pullRequestDetail.filesChanged}</h2>
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
          locale={locale}
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

function canMergeRequest(session: AuthSession, mergeRequest: MergeRequest, readiness: MergeReadiness | null): boolean {
  return isMutableSession(session) && mergeRequest.state === "open" && Boolean(readiness?.canMerge);
}

function pullRequestTabCounts(
  comments: CommentPage<MergeRequestComment> | null,
  commits: RevisionHistoryEntry[],
  readiness: MergeReadiness | null,
  diff: LoreDiff | null,
) {
  return {
    conversation: comments?.totalCount ?? null,
    commits: commits.length,
    checks: readiness?.statusChecks.length ?? null,
    files: diff?.files.length ?? null,
  };
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

function StateBadge({ dictionary, mergeRequest }: { dictionary: Dictionary; mergeRequest: MergeRequest }) {
  const state = mergeRequest.isDraft ? "draft" : mergeRequest.state;
  const label = mergeRequest.isDraft
    ? dictionary.pullRequestDrafts.badge
    : state === "open"
      ? dictionary.common.open
      : state === "merged"
        ? dictionary.common.merged
        : dictionary.common.closed;
  const icon =
    state === "draft" ? (
      <FilePenLine aria-hidden="true" size={16} />
    ) : state === "merged" ? (
      <GitMerge aria-hidden="true" size={16} />
    ) : state === "closed" ? (
      <GitPullRequestClosed aria-hidden="true" size={16} />
    ) : (
      <CircleDot aria-hidden="true" size={16} />
    );
  return (
    <span className={styles.stateBadge} data-state={state}>
      {icon}
      {label}
    </span>
  );
}

function BranchChip({ children }: { children: string }) {
  return <code className={styles.branchChip}>{children}</code>;
}

function TabLink({
  active,
  count,
  href,
  id,
  label,
  panelId,
  locale,
}: {
  active: boolean;
  count: number | null;
  href: string;
  id: string;
  label: string;
  panelId: string;
  locale: "en" | "ja";
}) {
  return (
    <Link aria-controls={panelId} aria-selected={active} className={styles.tab} href={href} id={id} role="tab">
      <span>{label}</span>
      {count !== null && <span className={styles.tabCount}>{abbreviateCount(count, locale)}</span>}
    </Link>
  );
}

function tabHref(path: string, tab: PullRequestTab): string {
  return `${path}?tab=${encodeURIComponent(tab)}`;
}

function CommitRow({
  commit,
  dictionary,
  locale,
  owner,
  repository,
}: {
  commit: RevisionHistoryEntry;
  dictionary: Dictionary;
  locale: "en" | "ja";
  owner: string;
  repository: string;
}) {
  const [copied, setCopied] = useState(false);
  const revisionPath = `${repositoryPath(locale, owner, repository)}/commit?revision=${encodeURIComponent(
    commit.revision,
  )}`;
  async function copyRevision() {
    if (!navigator.clipboard) return;
    await navigator.clipboard.writeText(commit.revision);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1600);
  }
  return (
    <li className={styles.commitRow}>
      <div className={styles.commitIdentity}>
        <Link href={revisionPath} className={styles.commitRevision}>
          {shortRevision(commit.revision)}
        </Link>
        <button
          aria-label={copied ? dictionary.pullRequestDetail.revisionCopied : dictionary.pullRequestDetail.copyRevision}
          className={styles.copyButton}
          onClick={() => void copyRevision()}
          type="button"
        >
          {copied ? <Check aria-hidden="true" size={14} /> : <Copy aria-hidden="true" size={14} />}
        </button>
        <span className={styles.commitNumber}>#{commit.number}</span>
      </div>
      {commit.message && <p>{commit.message}</p>}
    </li>
  );
}

type MergePanelProps = {
  busy: boolean;
  canMutate: boolean;
  authenticated: boolean;
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
  authenticated,
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
      <h2 id="merge-title">{dictionary.pullRequestDetail.mergePanelTitle}</h2>
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
        authenticated={authenticated}
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
      <div className={styles.statusRow} data-state="ready">
        <CircleCheck aria-hidden="true" size={20} />
        <strong>{dictionary.pullRequestDetail.ready}</strong>
      </div>
    );
  }
  return (
    <div className={styles.blockedStatus}>
      <div className={styles.statusRow} data-state="blocked">
        <CircleAlert aria-hidden="true" size={20} />
        <strong>{dictionary.pullRequestDetail.blocked}</strong>
      </div>
      {readiness.blockers.length > 0 && (
        <ul className={styles.blockers}>
          {readiness.blockers.map((blocker) => (
            <li key={blocker.code}>{blockerLabel(blocker.code, dictionary)}</li>
          ))}
        </ul>
      )}
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
  authenticated,
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
  authenticated: boolean;
  showStart: boolean;
  showPush: boolean;
}) {
  if (!canMutate) {
    return (
      <p className={styles.meta}>
        {!authenticated
          ? dictionary.pullRequestDetail.mergeRequiresAuth
          : readiness.canMerge
            ? dictionary.pullRequestDetail.blockers.writePermission
            : dictionary.pullRequestDetail.policyBlocked}
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
    case "draft":
      return dictionary.pullRequestDrafts.blocker;
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
    case "required_status_checks":
      return dictionary.commitStatuses.requiredContexts;
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
