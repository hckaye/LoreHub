import { LockKeyhole, ServerOff } from "lucide-react";

import { LabelManager } from "@/components/repositories/label-manager";
import { RepositoryPanel, RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getLabelPage } from "@/lib/lorehub-api";

type LabelsPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
};

export const dynamic = "force-dynamic";

export default async function LabelsPage({ params }: LabelsPageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, labels, session] = await Promise.all([
    getDictionary(locale),
    getLabelPage(owner, repository),
    getAuthSession(),
  ]);
  const copy = dictionary.labelsPage;
  return (
    <RepositorySection description={copy.description} title={copy.title}>
      <RepositoryPanel description={copy.description} title={copy.title}>
        {labels.ok ? (
          <LabelManager
            data={labels.data}
            dictionary={dictionary}
            owner={owner}
            repository={repository}
            session={session}
          />
        ) : labels.reason === "forbidden" || labels.reason === "not-found" ? (
          <EmptyState
            body={copy.forbiddenBody}
            icon={<LockKeyhole aria-hidden="true" />}
            title={copy.forbiddenTitle}
            tone="warning"
          />
        ) : (
          <EmptyState
            body={copy.unavailableBody}
            icon={<ServerOff aria-hidden="true" />}
            title={copy.unavailableTitle}
            tone="warning"
          />
        )}
      </RepositoryPanel>
    </RepositorySection>
  );
}
