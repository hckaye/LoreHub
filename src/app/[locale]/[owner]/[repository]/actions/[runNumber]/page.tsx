import { ServerOff } from "lucide-react";

import { ActionsRunDetail } from "@/components/repositories/actions-run-detail";
import { RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getActionRun, getActionWorkflows } from "@/lib/lorehub-api";

type ActionsRunPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string; runNumber: string }>;
};

export const dynamic = "force-dynamic";

export default async function ActionsRunPage({ params }: ActionsRunPageProps) {
  const { locale: value, owner, repository, runNumber: rawRunNumber } = await params;
  const locale = isLocale(value) ? value : "en";
  const runNumber = Number.parseInt(rawRunNumber, 10);
  const [dictionary, session, detail, workflows] = await Promise.all([
    getDictionary(locale),
    getAuthSession(),
    Number.isInteger(runNumber) ? getActionRun(owner, repository, runNumber) : Promise.resolve(null),
    getActionWorkflows(owner, repository),
  ]);
  const available = detail?.ok && workflows.ok;
  return (
    <RepositorySection description={dictionary.actionsPage.runDetailDescription} title={dictionary.actionsPage.title}>
      {available ? (
        <ActionsRunDetail
          canWrite={workflows.data.canWrite}
          detail={detail.data}
          dictionary={dictionary}
          locale={locale}
          owner={owner}
          repository={repository}
          session={session}
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
