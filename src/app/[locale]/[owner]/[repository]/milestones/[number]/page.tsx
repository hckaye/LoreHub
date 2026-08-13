import { ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { MilestoneDetail } from "@/components/repositories/milestone-detail";
import { RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getIssues, getMilestone, getPublicRepository } from "@/lib/lorehub-api";

type MilestoneDetailPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string; number: string }>;
  searchParams: Promise<{ state?: string }>;
};

export const dynamic = "force-dynamic";

export default async function MilestoneDetailPage({ params, searchParams }: MilestoneDetailPageProps) {
  const { locale: value, owner, repository, number: numberValue } = await params;
  const locale = isLocale(value) ? value : "en";
  const number = Number(numberValue);
  const state = parseState((await searchParams).state);
  const dictionary = await getDictionary(locale);
  const labels = dictionary.milestonesPage;
  if (!Number.isSafeInteger(number) || number < 1) notFound();

  const [repositoryResult, milestoneResult] = await Promise.all([
    getPublicRepository(owner, repository),
    getMilestone(owner, repository, number),
  ]);
  if (!repositoryResult.ok && repositoryResult.reason === "not-found") notFound();
  if (!milestoneResult.ok && milestoneResult.reason === "not-found") notFound();
  if (!milestoneResult.ok) {
    return (
      <RepositorySection title={labels.title}>
        <EmptyState
          body={labels.detailUnavailableBody}
          icon={<ServerOff aria-hidden="true" />}
          title={labels.detailUnavailableTitle}
          tone="warning"
        />
      </RepositorySection>
    );
  }

  const issuesResult = await getIssues(owner, repository, {
    milestone: String(number),
    state,
    perPage: 100,
  });
  const issues = issuesResult.ok ? issuesResult.data.issues : [];
  const openCount = milestoneResult.data.openIssueCount;
  const closedCount = milestoneResult.data.closedIssueCount;

  return (
    <RepositorySection title={labels.title}>
      <MilestoneDetail
        closedCount={closedCount}
        dictionary={dictionary}
        issues={issues}
        locale={locale}
        milestone={milestoneResult.data}
        openCount={openCount}
        owner={owner}
        repository={repository}
        state={state}
      />
    </RepositorySection>
  );
}

function parseState(value: string | undefined): "open" | "closed" {
  return value === "closed" ? "closed" : "open";
}
