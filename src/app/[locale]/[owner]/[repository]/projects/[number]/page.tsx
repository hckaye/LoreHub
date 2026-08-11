import { LockKeyhole, ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { ProjectBoard } from "@/components/repositories/project-board";
import { RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getProject } from "@/lib/lorehub-api";

type ProjectPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string; number: string }>;
};

export const dynamic = "force-dynamic";

export default async function ProjectPage({ params }: ProjectPageProps) {
  const { locale: value, owner, repository, number: numberValue } = await params;
  const locale = isLocale(value) ? value : "en";
  const number = Number(numberValue);
  if (!Number.isSafeInteger(number) || number < 1) notFound();
  const [dictionary, project, session] = await Promise.all([
    getDictionary(locale),
    getProject(owner, repository, number),
    getAuthSession(),
  ]);
  if (!project.ok && project.reason === "not-found") notFound();
  const labels = dictionary.projectsPage;
  return (
    <RepositorySection description={labels.description} title={labels.title}>
      {project.ok ? (
        <ProjectBoard
          dictionary={dictionary}
          locale={locale}
          owner={owner}
          project={project.data}
          repository={repository}
          session={session}
        />
      ) : project.reason === "forbidden" ? (
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
    </RepositorySection>
  );
}
