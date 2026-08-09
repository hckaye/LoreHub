import { BarChart3, ServerOff } from "lucide-react";

import { RepositoryFacts } from "@/components/repositories/repository-facts";
import { RepositoryPanel, RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getCIRuns, getIssues, getMergeRequests, getPublicRepository } from "@/lib/lorehub-api";

type InsightsPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
};

export const dynamic = "force-dynamic";

export default async function InsightsPage({ params }: InsightsPageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, repositoryResult, issues, pullRequests, actions] = await Promise.all([
    getDictionary(locale),
    getPublicRepository(owner, repository),
    getIssues(owner, repository, "open"),
    getMergeRequests(owner, repository, "open"),
    getCIRuns(owner, repository),
  ]);
  if (!repositoryResult.ok) {
    return (
      <EmptyState
        body={dictionary.home.apiUnavailableBody}
        icon={<ServerOff aria-hidden="true" />}
        title={dictionary.repository.unavailable}
        tone="warning"
      />
    );
  }
  return (
    <RepositorySection description={dictionary.insightsPage.description} title={dictionary.insightsPage.title}>
      <RepositoryPanel title={dictionary.insightsPage.overviewTitle}>
        <RepositoryFacts
          actionsRuns={actions.ok ? actions.data.length : null}
          dictionary={dictionary}
          openIssues={issues.ok ? issues.data.length : null}
          openPullRequests={pullRequests.ok ? pullRequests.data.length : null}
          repository={repositoryResult.data}
        />
      </RepositoryPanel>
      <EmptyState
        body={dictionary.insightsPage.emptyBody}
        icon={<BarChart3 aria-hidden="true" />}
        title={dictionary.insightsPage.emptyTitle}
      />
    </RepositorySection>
  );
}
