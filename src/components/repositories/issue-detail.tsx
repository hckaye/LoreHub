"use client";

import { CircleDot, CircleSlash2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Issue, IssueComment, Label, Milestone } from "@/lib/api-types";
import { deleteJson, patchJson, postJson, putJson } from "@/lib/auth-client";
import { assignIssueMilestone, removeIssueMilestone } from "@/lib/milestone-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { repositoryPath } from "@/lib/routes";

import { AuthRequired } from "../auth/auth-required";
import { IssueConversation } from "./issue-conversation";
import styles from "./issue-detail.module.css";
import { IssueSidebar } from "./issue-sidebar";

type IssueDetailProps = {
  comments: IssueComment[];
  commentsAvailable: boolean;
  dictionary: Dictionary;
  issue: Issue;
  labels: Label[];
  labelsAvailable: boolean;
  milestones: Milestone[];
  milestonesAvailable: boolean;
  locale: Locale;
  owner: string;
  repository: string;
  session: AuthSession;
};

export function IssueDetail(props: IssueDetailProps) {
  const { comments, commentsAvailable, dictionary, issue, labels, labelsAvailable } = props;
  const router = useRouter();
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const apiPath = issueAPIPath(props.owner, props.repository, issue.number);
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

  async function createComment(body: string): Promise<boolean> {
    if (!csrfToken) return false;
    setBusyAction("new-comment");
    setMessage(null);
    const result = await postJson<IssueComment>(`${apiPath}/comments`, { body }, csrfToken);
    setBusyAction(null);
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, dictionary));
      return false;
    }
    router.refresh();
    return true;
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
    router.refresh();
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

  const openedText = formatActivity(
    issue.state === "closed" && issue.closedBy && issue.closedAt
      ? dictionary.issueDetail.closedBy
      : dictionary.issueDetail.openedBy,
    issue.state === "closed" && issue.closedBy ? issue.closedBy : issue.author,
    issue.state === "closed" && issue.closedAt ? issue.closedAt : issue.createdAt,
    props.locale,
  );
  const returnTo = `${repositoryPath(props.locale, props.owner, props.repository, "issues")}/${issue.number}`;

  return (
    <div className={styles.page}>
      <header className={styles.heading}>
        <h1>
          {issue.title} <span>#{issue.number}</span>
        </h1>
        <div className={styles.statusLine}>
          <span className={styles.state} data-state={issue.state}>
            {issue.state === "open" ? <CircleDot aria-hidden="true" /> : <CircleSlash2 aria-hidden="true" />}
            {issue.state === "open" ? dictionary.common.open : dictionary.common.closed}
          </span>
          <span>{openedText}</span>
          <span>{dictionary.issueDetail.commentCount.replace("{count}", String(issue.commentCount))}</span>
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
          commentsAvailable={commentsAvailable}
          dictionary={dictionary}
          issue={issue}
          locale={props.locale}
          onCreateComment={createComment}
          onDeleteComment={deleteComment}
          onUpdateComment={updateComment}
          onUpdateIssue={updateIssue}
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
          onSetMilestone={setMilestone}
          onUpdateState={(state) => updateIssue({ state })}
          onToggleLabel={toggleLabel}
        />
      </div>
      {props.session.status !== "authenticated" && (
        <AuthRequired dictionary={dictionary} returnTo={returnTo} session={props.session} />
      )}
    </div>
  );
}

function issueAPIPath(owner: string, repository: string, number: number): string {
  return `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/issues/${number}`;
}

function formatActivity(template: string, author: string, value: string, locale: Locale): string {
  const date = new Intl.DateTimeFormat(locale, {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: "UTC",
  }).format(new Date(value));
  return template.replace("{author}", author).replace("{date}", date);
}
