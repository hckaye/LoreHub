import { ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { FileLockManager } from "@/components/repositories/file-lock-manager";
import { RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getFileLocks, getPublicRepository } from "@/lib/lorehub-api";

type FileLocksPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
  searchParams: Promise<{ branch?: string }>;
};

export const dynamic = "force-dynamic";

export default async function FileLocksPage({ params, searchParams }: FileLocksPageProps) {
  const [{ locale: value, owner, repository: slug }, query] = await Promise.all([params, searchParams]);
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session, repository, locks] = await Promise.all([
    getDictionary(locale),
    getAuthSession(),
    getPublicRepository(owner, slug),
    getFileLocks(owner, slug, query.branch),
  ]);
  if (!repository.ok && repository.reason === "not-found") notFound();
  if (!repository.ok || !locks.ok) {
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
    <RepositorySection description={dictionary.fileLocks.description} title={dictionary.fileLocks.title}>
      <FileLockManager
        dictionary={dictionary}
        key={locks.data.selectedBranch}
        locale={locale}
        page={locks.data}
        repository={repository.data}
        session={session}
      />
    </RepositorySection>
  );
}
