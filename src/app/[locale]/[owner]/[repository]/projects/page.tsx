import { LockKeyhole, ServerOff } from "lucide-react";

import { ProjectList } from "@/components/repositories/project-list";
import { RepositoryPanel, RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getProjects } from "@/lib/lorehub-api";

type ProjectsPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
};

export const dynamic = "force-dynamic";

export default async function ProjectsPage({ params }: ProjectsPageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, projects, session] = await Promise.all([
    getDictionary(locale),
    getProjects(owner, repository),
    getAuthSession(),
  ]);
  const labels = dictionary.projectsPage;
  return (
    <RepositorySection description={labels.description} title={labels.title}>
      <RepositoryPanel description={labels.description} title={labels.title}>
        {projects.ok ? (
          <ProjectList
            data={projects.data}
            dictionary={dictionary}
            locale={locale}
            owner={owner}
            repository={repository}
            session={session}
          />
        ) : projects.reason === "forbidden" || projects.reason === "not-found" ? (
          <EmptyState
            body={labels.forbiddenBody}
            icon={<LockKeyhole aria-hidden="true" />}
            title={labels.forbiddenTitle}
            tone="warning"
          />
        ) : (
          <EmptyState
            body={labels.unavailableBody}
            icon={<ServerOff aria-hidden="true" />}
            title={labels.unavailableTitle}
            tone="warning"
          />
        )}
      </RepositoryPanel>
    </RepositorySection>
  );
}
