import { KanbanSquare, ServerOff } from "lucide-react";

import { RepositoryFacts } from "@/components/repositories/repository-facts";
import { RepositoryPanel, RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getPublicRepository } from "@/lib/lorehub-api";

type ProjectsPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
};

export const dynamic = "force-dynamic";

export default async function ProjectsPage({ params }: ProjectsPageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, repositoryResult] = await Promise.all([
    getDictionary(locale),
    getPublicRepository(owner, repository),
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
    <RepositorySection description={dictionary.projectsPage.description} title={dictionary.projectsPage.title}>
      <RepositoryPanel description={dictionary.projectsPage.summaryBody} title={dictionary.projectsPage.summaryTitle}>
        <RepositoryFacts dictionary={dictionary} repository={repositoryResult.data} />
      </RepositoryPanel>
      <EmptyState
        body={dictionary.projectsPage.emptyBody}
        icon={<KanbanSquare aria-hidden="true" />}
        title={dictionary.projectsPage.emptyTitle}
      />
    </RepositorySection>
  );
}
