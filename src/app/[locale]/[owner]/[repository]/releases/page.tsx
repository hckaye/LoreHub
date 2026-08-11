import { LockKeyhole, ServerOff } from "lucide-react";

import { ReleaseList } from "@/components/repositories/release-list";
import { RepositoryPanel, RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getBranches, getReleases } from "@/lib/lorehub-api";

type ReleasesPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
  searchParams: Promise<{ page?: string }>;
};

export const dynamic = "force-dynamic";

export default async function ReleasesPage({ params, searchParams }: ReleasesPageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const query = await searchParams;
  const page = parsePage(query.page);
  const [dictionary, releases, branches, session] = await Promise.all([
    getDictionary(locale),
    getReleases(owner, repository, page),
    getBranches(owner, repository),
    getAuthSession(),
  ]);
  const labels = dictionary.releasesPage;
  return (
    <RepositorySection description={labels.description} title={labels.title}>
      <RepositoryPanel description={labels.description} title={labels.title}>
        {releases.ok ? (
          <ReleaseList
            branches={branches.ok ? branches.data : []}
            data={releases.data}
            dictionary={dictionary}
            locale={locale}
            owner={owner}
            repository={repository}
            session={session}
          />
        ) : releases.reason === "forbidden" || releases.reason === "not-found" ? (
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

function parsePage(value: string | undefined): number {
  if (!value || !/^\d+$/.test(value)) return 1;
  const page = Number(value);
  return Number.isSafeInteger(page) && page > 0 && page <= 1_000_000 ? page : 1;
}
