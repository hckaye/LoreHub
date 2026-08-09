import { GitBranch, ServerOff } from "lucide-react";

import { AuthRequired } from "@/components/auth/auth-required";
import { PullRequestForm } from "@/components/repositories/pull-request-form";
import { RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getBranches } from "@/lib/lorehub-api";
import { repositoryPath } from "@/lib/routes";

type NewPullRequestPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
};

export const dynamic = "force-dynamic";

export default async function NewPullRequestPage({ params }: NewPullRequestPageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  const branches = session.status === "authenticated" ? await getBranches(owner, repository) : null;
  return (
    <RepositorySection
      description={dictionary.pullRequestsPage.description}
      title={dictionary.pullRequestsPage.newPullRequest}
    >
      {session.status !== "authenticated" ? (
        <AuthRequired
          dictionary={dictionary}
          returnTo={`${repositoryPath(locale, owner, repository, "pulls")}/new`}
          session={session}
        />
      ) : branches?.ok ? (
        branches.data.length >= 2 ? (
          <PullRequestForm
            branches={branches.data}
            dictionary={dictionary}
            locale={locale}
            owner={owner}
            repository={repository}
            session={session}
          />
        ) : (
          <EmptyState
            body={dictionary.repository.branchesDescription}
            icon={<GitBranch aria-hidden="true" />}
            title={dictionary.repository.noBranches}
          />
        )
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
