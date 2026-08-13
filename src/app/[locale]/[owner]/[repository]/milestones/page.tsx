import { LockKeyhole, ServerOff } from "lucide-react";

import { MilestoneList } from "@/components/repositories/milestone-list";
import { RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { FilterTabs } from "@/components/ui/filter-tabs";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getMilestones } from "@/lib/lorehub-api";
import { repositoryMilestonesPath } from "@/lib/routes";

type MilestonesPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
  searchParams: Promise<{ page?: string; state?: string }>;
};

export const dynamic = "force-dynamic";

export default async function MilestonesPage({ params, searchParams }: MilestonesPageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const query = await searchParams;
  const state = parseState(query.state);
  const page = parsePage(query.page);
  const [dictionary, result, session, allResult] = await Promise.all([
    getDictionary(locale),
    getMilestones(owner, repository, state, page),
    getAuthSession(),
    getMilestones(owner, repository, "all", 1, 200),
  ]);
  const labels = dictionary.milestonesPage;
  const basePath = repositoryMilestonesPath(locale, owner, repository);
  const openCount = allResult.ok ? allResult.data.milestones.filter((m) => m.state === "open").length : 0;
  const closedCount = allResult.ok ? allResult.data.milestones.filter((m) => m.state === "closed").length : 0;
  const allCount = openCount + closedCount;
  return (
    <RepositorySection description={labels.description} title={labels.title}>
      <FilterTabs
        label={labels.title}
        tabs={[
          {
            active: state === "open",
            count: openCount,
            href: basePath,
            label: dictionary.common.open,
          },
          {
            active: state === "closed",
            count: closedCount,
            href: `${basePath}?state=closed`,
            label: dictionary.common.closed,
          },
          {
            active: state === "all",
            count: allCount,
            href: `${basePath}?state=all`,
            label: dictionary.common.all,
          },
        ]}
      />
      {result.ok ? (
        <MilestoneList
          data={result.data}
          dictionary={dictionary}
          locale={locale}
          owner={owner}
          repository={repository}
          session={session}
          state={state}
        />
      ) : result.reason === "forbidden" || result.reason === "not-found" ? (
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

function parseState(value: string | undefined): "open" | "closed" | "all" {
  return value === "closed" || value === "all" ? value : "open";
}

function parsePage(value: string | undefined): number {
  if (!value || !/^\d+$/.test(value)) return 1;
  const page = Number(value);
  return Number.isSafeInteger(page) && page > 0 && page <= 1_000_000 ? page : 1;
}
