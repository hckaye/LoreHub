import { ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { PullRequestDetail } from "@/components/repositories/pull-request-detail";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import {
  getLoreDiff,
  getMergeReadiness,
  getMergeRequest,
  getMergeRequestComments,
  getPublicRepository,
  getReviews,
  getRevisionHistory,
} from "@/lib/lorehub-api";

type PullRequestDetailPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string; number: string }>;
};

export const dynamic = "force-dynamic";

export default async function PullRequestDetailPage({ params }: PullRequestDetailPageProps) {
  const { locale: value, owner, repository: slug, number: numberValue } = await params;
  const locale = isLocale(value) ? value : "en";
  const number = Number.parseInt(numberValue, 10);
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
  const [session, readiness, reviews, comments, diff, history] = await Promise.all([
    getAuthSession(),
    getMergeReadiness(owner, slug, number),
    getReviews(owner, slug, number),
    getMergeRequestComments(owner, slug, number),
    getLoreDiff(owner, slug, mergeRequest.data.sourceRevision, mergeRequest.data.targetRevision),
    getRevisionHistory(owner, slug, {
      branch: mergeRequest.data.sourceBranch,
      revision: mergeRequest.data.sourceRevision,
    }),
  ]);
  return (
    <PullRequestDetail
      commits={history.ok ? history.data.entries : []}
      comments={comments.ok ? comments.data : []}
      commentsAvailable={comments.ok}
      diff={diff.ok ? diff.data : null}
      dictionary={dictionary}
      locale={locale}
      mergeRequest={mergeRequest.data}
      owner={owner}
      readiness={readiness.ok ? readiness.data : null}
      repository={slug}
      reviews={reviews.ok ? reviews.data : null}
      session={session}
    />
  );
}
