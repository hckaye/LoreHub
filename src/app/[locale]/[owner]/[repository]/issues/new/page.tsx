import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getPublicRepository } from "@/lib/lorehub-api";
import { repositoryPath } from "@/lib/routes";

import { AuthRequired } from "@/components/auth/auth-required";
import { IssueForm } from "@/components/repositories/issue-form";
import { EmptyState } from "@/components/ui/empty-state";
import { Archive } from "lucide-react";

type NewIssuePageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
};

export const dynamic = "force-dynamic";

export default async function NewIssuePage({ params }: NewIssuePageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session, repositoryResult] = await Promise.all([
    getDictionary(locale),
    getAuthSession(),
    getPublicRepository(owner, repository),
  ]);
  const archived = repositoryResult.ok && repositoryResult.data.archivedAt !== null;
  if (archived) {
    return (
      <EmptyState
        body={dictionary.repositoryLifecycle.banner}
        icon={<Archive aria-hidden="true" />}
        title={dictionary.repositoryLifecycle.badge}
        tone="warning"
      />
    );
  }
  if (session.status !== "authenticated") {
    return (
      <AuthRequired
        dictionary={dictionary}
        returnTo={`${repositoryPath(locale, owner, repository, "issues")}/new`}
        session={session}
      />
    );
  }
  return <IssueForm dictionary={dictionary} locale={locale} owner={owner} repository={repository} session={session} />;
}
