import { ServerOff } from "lucide-react";

import { CIRunList } from "@/components/repositories/ci-run-list";
import { RepositoryPanel, RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { FlashNotice } from "@/components/ui/flash-notice";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getCIRuns } from "@/lib/lorehub-api";

type ActionsPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
};

export const dynamic = "force-dynamic";

export default async function ActionsPage({ params }: ActionsPageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, runs] = await Promise.all([getDictionary(locale), getCIRuns(owner, repository)]);
  return (
    <RepositorySection description={dictionary.actionsPage.description} title={dictionary.actionsPage.title}>
      <FlashNotice body={dictionary.actionsPage.workflowNote} title={dictionary.actionsPage.title} tone="info" />
      <RepositoryPanel description={dictionary.repository.actionsDescription} title={dictionary.actionsPage.title}>
        {runs.ok ? (
          <CIRunList dictionary={dictionary} runs={runs.data} />
        ) : (
          <EmptyState
            body={dictionary.home.apiUnavailableBody}
            icon={<ServerOff aria-hidden="true" />}
            title={dictionary.repository.unavailable}
            tone="warning"
          />
        )}
      </RepositoryPanel>
    </RepositorySection>
  );
}
