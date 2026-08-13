import { ServerOff } from "lucide-react";
import { notFound, redirect } from "next/navigation";

import { PullRequestDetail, type PullRequestTab } from "@/components/repositories/pull-request-detail";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import type { APIResult } from "@/lib/api-types";
import { getAuthSession } from "@/lib/auth-api";
import {
  conversationCommentPageHref,
  lastConversationCommentPage,
  parseConversationCommentPage,
} from "@/lib/comment-pagination";
import {
  getAssignableUsers,
  getLabels,
  getLoreDiff,
  getMergeReadiness,
  getMergeRequest,
  getMergeRequestComments,
  getMergeRequestEvents,
  getMergeRequestMetadata,
  getMilestones,
  getPublicRepository,
  getReviewCandidates,
  getReviewRequests,
  getReviews,
  getReviewThreads,
  getRevisionHistory,
} from "@/lib/lorehub-api";
import { repositoryPath } from "@/lib/routes";

type PullRequestDetailPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string; number: string }>;
  searchParams: Promise<{ comment_page?: string | string[]; tab?: string | string[] }>;
};

export const dynamic = "force-dynamic";

export default async function PullRequestDetailPage({ params, searchParams }: PullRequestDetailPageProps) {
  const { locale: value, owner, repository: slug, number: numberValue } = await params;
  const locale = isLocale(value) ? value : "en";
  const number = Number(numberValue);
  const query = await searchParams;
  const commentPage = parseConversationCommentPage(query.comment_page);
  const tab = parsePullRequestTab(query.tab);
  const dictionary = await getDictionary(locale);
  if (!Number.isSafeInteger(number) || number < 1) notFound();
  const repository = await getPublicRepository(owner, slug);
  if (!repository.ok && repository.reason === "not-found") notFound();
  if (!repository.ok) {
    return (
      <EmptyState
        body={dictionary.home.apiUnavailableBody}
        icon={<ServerOff />}
        title={dictionary.repository.unavailable}
      />
    );
  }
  const mergeRequest = await getMergeRequest(owner, slug, number);
  if (!mergeRequest.ok && mergeRequest.reason === "not-found") notFound();
  if (!mergeRequest.ok) {
    return (
      <EmptyState
        body={dictionary.home.apiUnavailableBody}
        icon={<ServerOff />}
        title={dictionary.repository.unavailable}
      />
    );
  }
  const [
    session,
    readiness,
    reviews,
    reviewRequests,
    reviewCandidates,
    comments,
    events,
    reviewThreads,
    diff,
    history,
    metadata,
    labels,
    assignees,
    milestones,
  ] = await Promise.all([
    getAuthSession(),
    getMergeReadiness(owner, slug, number),
    getReviews(owner, slug, number),
    getReviewRequests(owner, slug, number),
    getReviewCandidates(owner, slug, number),
    getMergeRequestComments(owner, slug, number, commentPage),
    getMergeRequestEvents(owner, slug, number),
    getReviewThreads(owner, slug, number),
    getLoreDiff(owner, slug, mergeRequest.data.targetRevision, mergeRequest.data.sourceRevision),
    getRevisionHistory(owner, slug, {
      branch: mergeRequest.data.sourceBranch,
      revision: mergeRequest.data.sourceRevision,
    }),
    getMergeRequestMetadata(owner, slug, number),
    getLabels(owner, slug),
    getAssignableUsers(owner, slug),
    getMilestones(owner, slug, "all", 1, 100),
  ]);
  redirectInvalidCommentPage(comments, commentPage, locale, owner, slug, number);
  return (
    <PullRequestDetail
      commits={historyEntries(history)}
      comments={resultData(comments, null)}
      assignees={assigneeItems(assignees)}
      assigneesAvailable={assignees.ok}
      diff={resultData(diff, null)}
      dictionary={dictionary}
      events={events.ok ? events.data : null}
      labels={resultData(labels, [])}
      labelsAvailable={labels.ok}
      locale={locale}
      mergeRequest={mergeRequest.data}
      metadata={resultData(metadata, null)}
      milestones={milestoneItems(milestones)}
      milestonesAvailable={milestones.ok}
      owner={owner}
      initialTab={tab}
      readiness={resultData(readiness, null)}
      readinessUnavailableReason={
        !readiness.ok && (readiness.reason === "forbidden" || readiness.reason === "unauthorized")
          ? "forbidden"
          : "unavailable"
      }
      repository={slug}
      reviews={resultData(reviews, null)}
      reviewCandidates={resultData(reviewCandidates, [])}
      reviewRequests={resultData(reviewRequests, null)}
      pendingReview={reviewThreads.ok ? reviewThreads.data.pendingReview : null}
      reviewThreads={reviewThreadData(reviewThreads)}
      reviewThreadsAvailable={reviewThreads.ok}
      session={session}
    />
  );
}

function parsePullRequestTab(value: string | string[] | undefined): PullRequestTab {
  const tab = Array.isArray(value) ? value[0] : value;
  return tab === "commits" || tab === "checks" || tab === "files" ? tab : "conversation";
}

function redirectInvalidCommentPage(
  comments: Awaited<ReturnType<typeof getMergeRequestComments>>,
  commentPage: number,
  locale: "en" | "ja",
  owner: string,
  repository: string,
  number: number,
) {
  if (!comments.ok) return;
  const lastPage = lastConversationCommentPage(comments.data.totalCount, comments.data.perPage);
  if (commentPage <= lastPage) return;
  const basePath = `${repositoryPath(locale, owner, repository, "pulls")}/${number}`;
  redirect(conversationCommentPageHref(basePath, lastPage));
}

function reviewThreadData(result: Awaited<ReturnType<typeof getReviewThreads>>) {
  return result.ok ? result.data.threads : [];
}

function assigneeItems(result: Awaited<ReturnType<typeof getAssignableUsers>>) {
  return result.ok ? result.data.items : [];
}

function milestoneItems(result: Awaited<ReturnType<typeof getMilestones>>) {
  return result.ok ? result.data.milestones : [];
}

function historyEntries(result: Awaited<ReturnType<typeof getRevisionHistory>>) {
  return result.ok ? result.data.entries : [];
}

function resultData<T, F>(result: APIResult<T>, fallback: F): T | F {
  return result.ok ? result.data : fallback;
}
