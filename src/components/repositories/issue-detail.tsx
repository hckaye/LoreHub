"use client";

import { CircleCheck, CircleDot } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { Assignee, AuthSession, Issue, IssueComment, Label, Milestone } from "@/lib/api-types";
import { deleteJson, patchJson, postJson, putJson } from "@/lib/auth-client";
import type { CommentPage } from "@/lib/comment-page-types";
import {
  conversationCommentPageHref,
  conversationCommentPageSize,
  lastConversationCommentPage,
} from "@/lib/comment-pagination";
import { assignIssueUser, removeIssueUser } from "@/lib/issue-assignee-client";
import { assignIssueMilestone, removeIssueMilestone } from "@/lib/milestone-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { localizedPath, repositoryPath } from "@/lib/routes";

import { IssueConversation } from "./issue-conversation";
import styles from "./issue-detail.module.css";
import { IssueSidebar } from "./issue-sidebar";
import { RelativeTime } from "./issue-timeline-item";

type IssueDetailProps = {
  comments: CommentPage<IssueComment> | null;
  dictionary: Dictionary;
  issue: Issue;
  labels: Label[];
  labelsAvailable: boolean;
  milestones: Milestone[];
  milestonesAvailable: boolean;
  assignableUsers: Assignee[];
  assigneesAvailable: boolean;
  locale: Locale;
  owner: string;
  repository: string;
  session: AuthSession;
};

export function IssueDetail(props: IssueDetailProps) {
  const { comments, dictionary, issue, labels, labelsAvailable } = props;
  const router = useRouter();
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const apiPath = issueAPIPath(props.owner, props.repository, issue.number);
  const returnTo = `${repositoryPath(props.locale, props.owner, props.repository, "issues")}/${issue.number}`;
  const csrfToken = props.session.status === "authenticated" ? props.session.csrfToken : "";

  async function updateIssue(input: Partial<Pick<Issue, "title" | "body" | "state">>): Promise<boolean> {
    if (!csrfToken) return false;
    setBusyAction("issue");
    setMessage(null);
    const result = await patchJson<Issue>(apiPath, input, csrfToken, {
      "If-Match": `"${issue.updatedAt}"`,
    });
    setBusyAction(null);
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, dictionary));
      return false;
    }
    router.refresh();
    return true;
  }

  async function submitComment(body: string, nextState: "open" | "closed" | null): Promise<boolean> {
    if (!csrfToken) return false;
    setBusyAction("new-comment");
    setMessage(null);
    if (nextState) {
      const stateResult = await patchJson<Issue>(apiPath, { state: nextState }, csrfToken, {
        "If-Match": `"${issue.updatedAt}"`,
      });
      if (!stateResult.ok) {
        setBusyAction(null);
        setMessage(mutationFailureMessage(stateResult.kind, dictionary));
        return false;
      }
    }
    if (!body.trim()) {
      setBusyAction(null);
      router.refresh();
      return true;
    }
    const result = await postJson<IssueComment>(`${apiPath}/comments`, { body }, csrfToken);
    setBusyAction(null);
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, dictionary));
      return false;
    }
    showCommentPage((comments?.totalCount ?? issue.commentCount) + 1, true);
    return true;
  }

  function showCommentPage(totalCount: number, showLastPage = false) {
    const currentPage = comments?.page ?? 1;
    const perPage = comments?.perPage ?? conversationCommentPageSize;
    const lastPage = lastConversationCommentPage(totalCount, perPage);
    const targetPage = showLastPage ? lastPage : Math.min(currentPage, lastPage);
    if (targetPage !== currentPage) {
      router.push(conversationCommentPageHref(returnTo, targetPage));
      return;
    }
    router.refresh();
  }

  async function updateComment(commentID: string, body: string): Promise<boolean> {
    if (!csrfToken) return false;
    setBusyAction(commentID);
    setMessage(null);
    const result = await patchJson<IssueComment>(
      `${apiPath}/comments/${encodeURIComponent(commentID)}`,
      { body },
      csrfToken,
    );
    setBusyAction(null);
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, dictionary));
      return false;
    }
    router.refresh();
    return true;
  }

  async function deleteComment(commentID: string): Promise<boolean> {
    if (!csrfToken) return false;
    setBusyAction(commentID);
    setMessage(null);
    const result = await deleteJson<null>(`${apiPath}/comments/${encodeURIComponent(commentID)}`, csrfToken);
    setBusyAction(null);
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, dictionary));
      return false;
    }
    showCommentPage(Math.max(0, (comments?.totalCount ?? issue.commentCount) - 1));
    return true;
  }

  async function toggleLabel(label: Label, selected: boolean): Promise<void> {
    if (!csrfToken) return;
    setBusyAction(label.id);
    setMessage(null);
    const path = `${apiPath}/labels/${encodeURIComponent(label.id)}`;
    const result = selected
      ? await putJson<Label>(path, undefined, csrfToken)
      : await deleteJson<null>(path, csrfToken);
    setBusyAction(null);
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, dictionary));
      return;
    }
    router.refresh();
  }

  async function setMilestone(milestoneNumber: number | null): Promise<void> {
    if (!csrfToken) return;
    setBusyAction("milestone");
    setMessage(null);
    const result =
      milestoneNumber === null
        ? await removeIssueMilestone(props.owner, props.repository, issue.number, csrfToken)
        : await assignIssueMilestone(props.owner, props.repository, issue.number, milestoneNumber, csrfToken);
    setBusyAction(null);
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, dictionary));
      return;
    }
    router.refresh();
  }

  async function setAssignee(assignee: Assignee, selected: boolean): Promise<void> {
    if (!csrfToken) return;
    setBusyAction(`assignee:${assignee.id}`);
    setMessage(null);
    const result = selected
      ? await assignIssueUser(props.owner, props.repository, issue.number, assignee.username, csrfToken)
      : await removeIssueUser(props.owner, props.repository, issue.number, assignee.username, csrfToken);
    setBusyAction(null);
    if (!result.ok) {
      setMessage(
        result.code === "assignee_limit"
          ? dictionary.issueAssignees.limit
          : mutationFailureMessage(result.kind, dictionary),
      );
      return;
    }
    router.refresh();
  }

  const closed = issue.state === "closed" && issue.closedBy && issue.closedAt;
  const actor = closed && issue.closedBy ? issue.closedBy : issue.author;
  const timestamp = closed && issue.closedAt ? issue.closedAt : issue.createdAt;
  const activity = closed ? dictionary.issueDetail.closedThisIssue : dictionary.issueDetail.openedThisIssue;
  return (
    <div className={styles.page}>
      <header className={styles.heading}>
        <h1>
          {issue.title} <span>#{issue.number}</span>
        </h1>
        <div className={styles.statusLine}>
          <span className={styles.state} data-state={issue.state}>
            {issue.state === "open" ? <CircleDot aria-hidden="true" /> : <CircleCheck aria-hidden="true" />}
            {issue.state === "open" ? dictionary.common.open : dictionary.common.closed}
          </span>
          <span className={styles.activity}>
            <Link className={styles.author} href={localizedPath(props.locale, actor)}>
              {actor}
            </Link>
            <RelativeTime locale={props.locale} template={activity} value={timestamp} />
            <span aria-hidden="true">·</span>
            <span>{dictionary.issueDetail.commentCount.replace("{count}", String(issue.commentCount))}</span>
          </span>
        </div>
      </header>
      {message && (
        <p className={styles.notice} role="alert">
          {message}
        </p>
      )}
      <div className={styles.columns}>
        <IssueConversation
          busyAction={busyAction}
          comments={comments}
          basePath={returnTo}
          dictionary={dictionary}
          issue={issue}
          locale={props.locale}
          onDeleteComment={deleteComment}
          onSubmitComment={submitComment}
          onUpdateComment={updateComment}
          onUpdateIssue={updateIssue}
          owner={props.owner}
          repository={props.repository}
          session={props.session}
        />
        <IssueSidebar
          busyAction={busyAction}
          dictionary={dictionary}
          issue={issue}
          labels={labels}
          labelsAvailable={labelsAvailable}
          milestones={props.milestones}
          milestonesAvailable={props.milestonesAvailable}
          assignableUsers={props.assignableUsers}
          assigneesAvailable={props.assigneesAvailable}
          locale={props.locale}
          onSetAssignee={setAssignee}
          onSetMilestone={setMilestone}
          onToggleLabel={toggleLabel}
          owner={props.owner}
          repository={props.repository}
        />
      </div>
    </div>
  );
}

function issueAPIPath(owner: string, repository: string, number: number): string {
  return `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/issues/${number}`;
}
