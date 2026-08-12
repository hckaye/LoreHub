import { ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { AuthRequired } from "@/components/auth/auth-required";
import { DiscussionForm } from "@/components/discussions/discussion-form";
import { RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getDiscussionCategories } from "@/lib/lorehub-api";
import { repositoryPath } from "@/lib/routes";

type NewDiscussionPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
};

export const dynamic = "force-dynamic";

export default async function NewDiscussionPage({ params }: NewDiscussionPageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  if (session.status !== "authenticated") {
    return (
      <RepositorySection
        description={dictionary.discussionsPage.description}
        title={dictionary.discussionsPage.createTitle}
      >
        <AuthRequired
          dictionary={dictionary}
          returnTo={`${repositoryPath(locale, owner, repository, "discussions")}/new`}
          session={session}
        />
      </RepositorySection>
    );
  }
  const categories = await getDiscussionCategories(owner, repository);
  if (!categories.ok && categories.reason === "not-found") notFound();
  if (!categories.ok) {
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
    <RepositorySection
      description={dictionary.discussionsPage.createDescription}
      title={dictionary.discussionsPage.createTitle}
    >
      <DiscussionForm
        categories={categories.data.categories}
        dictionary={dictionary}
        locale={locale}
        owner={owner}
        repository={repository}
        session={session}
        viewerCanModerate={categories.data.viewerCanModerate}
      />
    </RepositorySection>
  );
}
