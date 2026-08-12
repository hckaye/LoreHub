import { ServerOff } from "lucide-react";
import { notFound, redirect } from "next/navigation";

import { IssueDetail } from "@/components/repositories/issue-detail";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import {
  conversationCommentPageHref,
  lastConversationCommentPage,
  parseConversationCommentPage,
} from "@/lib/comment-pagination";
import {
  getAssignableUsers,
  getIssue,
  getIssueComments,
  getLabels,
  getMilestones,
  getPublicRepository,
} from "@/lib/lorehub-api";
import { repositoryPath } from "@/lib/routes";

type IssueDetailPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string; number: string }>;
  searchParams: Promise<{ comment_page?: string | string[] }>;
};

export const dynamic = "force-dynamic";

export default async function IssueDetailPage({ params, searchParams }: IssueDetailPageProps) {
  const { locale: value, owner, repository, number: numberValue } = await params;
  const locale = isLocale(value) ? value : "en";
  const number = Number(numberValue);
  const commentPage = parseConversationCommentPage((await searchParams).comment_page);
  const dictionary = await getDictionary(locale);
  if (!Number.isSafeInteger(number) || number < 1) notFound();

  const repositoryResult = await getPublicRepository(owner, repository);
  if (!repositoryResult.ok && repositoryResult.reason === "not-found") notFound();
  if (!repositoryResult.ok) return unavailable(dictionary);

  const issue = await getIssue(owner, repository, number);
  if (!issue.ok && issue.reason === "not-found") notFound();
  if (!issue.ok) return unavailable(dictionary);

  const [comments, labels, milestones, assignees, session] = await Promise.all([
    getIssueComments(owner, repository, number, commentPage),
    getLabels(owner, repository),
    getMilestones(owner, repository, "all", 1, 100),
    getAssignableUsers(owner, repository),
    getAuthSession(),
  ]);
  redirectInvalidCommentPage(comments, commentPage, locale, owner, repository, number);
  return (
    <IssueDetail
      comments={comments.ok ? comments.data : null}
      dictionary={dictionary}
      issue={issue.data}
      labels={labels.ok ? labels.data : []}
      labelsAvailable={labels.ok}
      locale={locale}
      owner={owner}
      repository={repository}
      session={session}
      milestones={milestones.ok ? milestones.data.milestones : []}
      milestonesAvailable={milestones.ok}
      assignableUsers={assignees.ok ? assignees.data.items : []}
      assigneesAvailable={assignees.ok}
    />
  );
}

function redirectInvalidCommentPage(
  comments: Awaited<ReturnType<typeof getIssueComments>>,
  commentPage: number,
  locale: "en" | "ja",
  owner: string,
  repository: string,
  number: number,
) {
  if (!comments.ok) return;
  const lastPage = lastConversationCommentPage(comments.data.totalCount, comments.data.perPage);
  if (commentPage <= lastPage) return;
  const basePath = `${repositoryPath(locale, owner, repository, "issues")}/${number}`;
  redirect(conversationCommentPageHref(basePath, lastPage));
}

function unavailable(dictionary: Awaited<ReturnType<typeof getDictionary>>) {
  return (
    <EmptyState
      body={dictionary.home.apiUnavailableBody}
      icon={<ServerOff aria-hidden="true" />}
      title={dictionary.repository.unavailable}
      tone="warning"
    />
  );
}
