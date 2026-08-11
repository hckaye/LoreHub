import { ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { BranchManagement } from "@/components/repositories/branch-management";
import { RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getBranchOverview, getBranchRules, getPublicRepository } from "@/lib/lorehub-api";

type BranchesPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
};

export const dynamic = "force-dynamic";

export default async function BranchesPage({ params }: BranchesPageProps) {
  const { locale: value, owner, repository: slug } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session, repository, overview, rules] = await Promise.all([
    getDictionary(locale),
    getAuthSession(),
    getPublicRepository(owner, slug),
    getBranchOverview(owner, slug),
    getBranchRules(owner, slug),
  ]);
  if (!repository.ok && repository.reason === "not-found") notFound();
  if (!repository.ok || !overview.ok || !rules.ok) {
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
    <RepositorySection description={dictionary.branchManagement.description} title={dictionary.branchManagement.title}>
      <BranchManagement
        dictionary={dictionary}
        initialRules={rules.data}
        locale={locale}
        overview={overview.data}
        repository={repository.data}
        session={session}
      />
    </RepositorySection>
  );
}
