import { ServerOff } from "lucide-react";

import { ActionsDashboard } from "@/components/repositories/actions-dashboard";
import { RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getActionRuns, getActionWorkflows, getDeployments } from "@/lib/lorehub-api";

type ActionsPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
};

export const dynamic = "force-dynamic";

export default async function ActionsPage({ params }: ActionsPageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session, workflows, runs, deployments] = await Promise.all([
    getDictionary(locale),
    getAuthSession(),
    getActionWorkflows(owner, repository),
    getActionRuns(owner, repository),
    getDeployments(owner, repository),
  ]);
  const available = workflows.ok && runs.ok;
  return (
    <RepositorySection description={dictionary.actionsPage.description} title={dictionary.actionsPage.title}>
      {available ? (
        <ActionsDashboard
          canWrite={workflows.data.canWrite}
          dictionary={dictionary}
          deployments={deployments.ok ? deployments.data : []}
          deploymentsAvailable={deployments.ok}
          locale={locale}
          owner={owner}
          repository={repository}
          runs={runs.data.runs}
          session={session}
          workflows={workflows.data.workflows}
        />
      ) : (
        <EmptyState
          body={dictionary.home.apiUnavailableBody}
          icon={<ServerOff aria-hidden="true" />}
          title={dictionary.repository.unavailable}
          tone="warning"
        />
      )}
    </RepositorySection>
  );
}
