import { ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { IssueDetail } from "@/components/repositories/issue-detail";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import {
  getAssignableUsers,
  getIssue,
  getIssueComments,
  getLabels,
  getMilestones,
  getPublicRepository,
} from "@/lib/lorehub-api";

type IssueDetailPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string; number: string }>;
};

export const dynamic = "force-dynamic";

export default async function IssueDetailPage({ params }: IssueDetailPageProps) {
  const { locale: value, owner, repository, number: numberValue } = await params;
  const locale = isLocale(value) ? value : "en";
  const number = Number(numberValue);
  const dictionary = await getDictionary(locale);
  if (!Number.isSafeInteger(number) || number < 1) notFound();

  const repositoryResult = await getPublicRepository(owner, repository);
  if (!repositoryResult.ok && repositoryResult.reason === "not-found") notFound();
  if (!repositoryResult.ok) return unavailable(dictionary);

  const issue = await getIssue(owner, repository, number);
  if (!issue.ok && issue.reason === "not-found") notFound();
  if (!issue.ok) return unavailable(dictionary);

  const [comments, labels, milestones, assignees, session] = await Promise.all([
    getIssueComments(owner, repository, number),
    getLabels(owner, repository),
    getMilestones(owner, repository, "all", 1, 100),
    getAssignableUsers(owner, repository),
    getAuthSession(),
  ]);
  return (
    <IssueDetail
      comments={comments.ok ? comments.data : []}
      commentsAvailable={comments.ok}
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
